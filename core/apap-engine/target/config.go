// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	ssh2 "github.com/Arm-Debug/apap-cli/apap-engine/ssh"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type TargetConfig struct {
	SchemaVersion string
	Default       string
	Targets       map[string]Target
}

type TargetManagerService interface {
	ReadTargetConfig() (*TargetConfig, error)
	GetDefaultTarget() (Target, error)
	GetTarget(targetName string) (Target, error)
	AddTarget(name string, newTarget Target) error
	RemoveTarget(targetName string) error
	RemoveAllTargets() error
	SetDefaultTarget(name string) error
	UpdateTarget(name string, fields *UpdateTargetFields) error
	GetDefaultTargetName() (string, error)
}

const DefaultTargetFilename = "targets.json"

var userConfigDir, _ = userdirs.ConfigDir()
var DefaultTargetFilepath string = filepath.Join(userConfigDir, DefaultTargetFilename)
var targetsSchemaV1 = "1.0.0"

type jsonTargetConfig struct {
	SchemaVersion string                `json:"schema_version,omitempty"` // the file’s contract
	Default       string                `json:"default"`
	Targets       map[string]JSONTarget `json:"targets"`
}

type UpdateTargetFields struct {
	Name          string
	DefaultFlag   bool
	UpdatedTarget Target
}

type TargetManager struct {
	targetConfigFilepath string
	localHostVerifier    localHostVerifier
}

func NewDefaultTargetManager() *TargetManager {
	return NewTargetManager(DefaultTargetFilepath, &concreteLocalhostVerifier{})
}

func NewTargetManager(path string, lhv localHostVerifier) *TargetManager {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	return &TargetManager{targetConfigFilepath: absPath, localHostVerifier: lhv}
}

// Helper function that adds the local host to the target config
func AddLocalHostToTargetConfig(config *TargetConfig) *TargetConfig {
	config.Targets[LocalhostName] = &LocalTarget{}

	_, defaultTargetExists := config.Targets[config.Default]
	if !defaultTargetExists {
		config.Default = LocalhostName
	}

	return config
}

func (dm *TargetManager) ReadTargetConfig() (*TargetConfig, error) {
	config, err := dm.readTargetConfigFile()
	if err != nil {
		return nil, err
	}

	_, localTargetExists := config.Targets[LocalhostName]
	if localTargetExists {
		return nil, message.New(message.EngineTargetConfigLocalhostAlreadyExists)
	}

	// Localhost is added to the config file dynamically at read time
	if dm.localHostVerifier.IsLocalhostSupported() {
		return AddLocalHostToTargetConfig(config), nil
	}

	return config, nil
}

func (dm *TargetManager) readTargetConfigFile() (*TargetConfig, error) {
	data, err := os.ReadFile(dm.targetConfigFilepath)
	if err != nil {
		// Return empty map if file does not exist
		if os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(dm.targetConfigFilepath), perms.LocalDirPerm); err != nil {
				metadata := map[string]string{"path": dm.targetConfigFilepath, "parentDir": filepath.Dir(dm.targetConfigFilepath)}
				return nil, message.New(message.EngineTargetConfigCreateFailure).WithMetadata(metadata).WithCause(err)
			}
			return &TargetConfig{Targets: make(map[string]Target)}, nil
		}
		metadata := map[string]string{"path": dm.targetConfigFilepath}
		return nil, message.New(message.EngineTargetConfigReadFailure).WithMetadata(metadata).WithCause(err)
	}

	if len(bytes.TrimSpace(data)) == 0 {
		return &TargetConfig{Targets: make(map[string]Target)}, nil
	}

	var jsonConfig jsonTargetConfig
	err = json.Unmarshal(data, &jsonConfig)
	if err != nil {
		metadata := map[string]string{"path": dm.targetConfigFilepath}
		return nil, message.New(message.EngineTargetConfigParseFailure).WithMetadata(metadata).WithCause(err)
	}

	var config TargetConfig
	config.Default = jsonConfig.Default
	config.Targets = make(map[string]Target)
	config.SchemaVersion = jsonConfig.SchemaVersion // preserve contract

	for k, v := range jsonConfig.Targets {
		config.Targets[k], err = EngineTargetFromJSON(v)
		if err != nil {
			metadata := map[string]string{"path": dm.targetConfigFilepath, "target": k}
			return nil, message.New(message.EngineTargetConfigParseTargetFailure).WithMetadata(metadata).WithCause(err)
		}
	}

	return &config, nil
}

func (dm *TargetManager) GetDefaultTarget() (Target, error) {
	config, err := dm.ReadTargetConfig()
	if err != nil {
		return nil, err
	}

	target, exists := config.Targets[config.Default]
	if !exists {
		return nil, message.New(message.EngineTargetConfigMissingDefault)
	}

	return target, nil
}

func (dm *TargetManager) GetTarget(targetName string) (Target, error) {
	// targetName could be a JSON string representing a target
	// or a plain string representing the target name
	_, err := TryUnmarshalJSONTargetString(targetName)
	if err != nil {
		return nil, err
	}

	config, err := dm.ReadTargetConfig()
	if err != nil {
		return nil, err
	}

	var exists bool
	var tgt Target
	if len(targetName) == 0 {
		// If no name was specified, attempt to retrieve the default
		tgt, exists = config.Targets[config.Default]
		if !exists {
			return nil, message.New(message.EngineTargetConfigMissingDefault)
		}
	} else {
		// Otherwise, attempt to retrieve the specified target
		tgt, exists = config.Targets[targetName]
		if !exists {
			return nil, message.New(message.EngineTargetConfigDoesNotExist).WithMetadata(map[string]string{"name": targetName})
		}
	}

	return tgt, nil
}

func (dm *TargetManager) writeTargetConfig(config *TargetConfig) error {
	jsonStruct := jsonTargetConfig{
		SchemaVersion: config.SchemaVersion,
		Default:       config.Default,
		Targets:       make(map[string]JSONTarget),
	}
	for k, v := range config.Targets {
		if cfg, err := JSONTargetFromEngine(v); err != nil {
			metadata := map[string]string{"path": dm.targetConfigFilepath}
			return message.New(message.EngineTargetConfigWriteFailure).WithMetadata(metadata).WithCause(err)
		} else {
			switch v.(type) {
			case *SSHTarget, *AndroidTarget:
				jsonStruct.Targets[k] = cfg
			}
		}
	}

	if jsonStruct.SchemaVersion == "" {
		jsonStruct.SchemaVersion = targetsSchemaV1
	}

	data, err := json.MarshalIndent(jsonStruct, "", "  ")
	if err != nil {
		return message.New(message.CommonUnknownError).WithCause(err)
	}

	if err := os.WriteFile(dm.targetConfigFilepath, data, perms.LocalFilePerm); err != nil {
		metadata := map[string]string{"path": dm.targetConfigFilepath}
		return message.New(message.EngineTargetConfigWriteFailure).WithMetadata(metadata).WithCause(err)
	}
	return nil
}

func (dm *TargetManager) AddTarget(name string, newTarget Target) error {
	if err := newTarget.Validate(name); err != nil {
		return err
	}

	config, err := dm.ReadTargetConfig()
	if err != nil {
		return err
	}

	if _, exists := config.Targets[name]; exists {
		return message.New(message.EngineTargetConfigAlreadyExists).WithMetadata(map[string]string{"name": name})
	}

	_, defaultTargetExists := config.Targets[config.Default]
	if !defaultTargetExists || isNameReserved(config.Default) {
		config.Default = name
	}

	config.Targets[name] = newTarget

	return dm.writeTargetConfig(config)
}

func (dm *TargetManager) RemoveTarget(targetName string) error {
	config, err := dm.ReadTargetConfig()
	if err != nil {
		return err
	}

	if _, exists := config.Targets[targetName]; !exists {
		return message.New(message.EngineTargetConfigDoesNotExist).WithMetadata(map[string]string{"name": targetName})
	}

	if isNameReserved(targetName) {
		return message.New(message.EngineTargetConfigCannotRemoveReserved).WithMetadata(map[string]string{"name": targetName})
	}

	delete(config.Targets, targetName)

	if config.Default == targetName {
		config.Default = ""
	}

	return dm.writeTargetConfig(config)
}

func (dm *TargetManager) RemoveAllTargets() error {
	return dm.writeTargetConfig(&TargetConfig{})
}

func (dm *TargetManager) SetDefaultTarget(targetName string) error {
	config, err := dm.ReadTargetConfig()
	if err != nil {
		return err
	}

	if _, exists := config.Targets[targetName]; !exists {
		return message.New(message.EngineTargetConfigDoesNotExist).WithMetadata(map[string]string{"name": targetName})
	}

	config.Default = targetName

	return dm.writeTargetConfig(config)
}

func (dm *TargetManager) GetDefaultTargetName() (string, error) {
	config, err := dm.ReadTargetConfig()
	if err != nil {
		return "", err
	}

	if config.Default == "" {
		return "", message.New(message.EngineTargetConfigMissingDefault)
	}

	return config.Default, nil
}

func (dm *TargetManager) UpdateTarget(oldTargetName string, updateFields *UpdateTargetFields) error {
	config, err := dm.ReadTargetConfig()
	if err != nil {
		return err
	}

	// Check old target exists
	if _, exists := config.Targets[oldTargetName]; !exists {
		return message.New(message.EngineTargetConfigDoesNotExist).WithMetadata(map[string]string{"name": oldTargetName})
	}

	if updateFields.Name == "" {
		updateFields.Name = oldTargetName
	}

	// Check new name is not already taken
	if _, exists := config.Targets[updateFields.Name]; exists {
		if updateFields.Name != oldTargetName {
			return message.New(message.EngineTargetConfigAlreadyExists).WithMetadata(map[string]string{"name": updateFields.Name})
		}
	}

	// Validate target properties
	if err := updateFields.UpdatedTarget.Validate(updateFields.Name); err != nil {
		return err
	}

	// All checks have passed - safe to remove and add target
	delete(config.Targets, oldTargetName)
	config.Targets[updateFields.Name] = updateFields.UpdatedTarget

	// If old target was default - migrate default to the updated target
	migrateDefault := config.Default == oldTargetName
	if updateFields.DefaultFlag || migrateDefault {
		config.Default = updateFields.Name
	}

	return dm.writeTargetConfig(config)
}

func validateHost(jump SSHHostConfig) error {
	host := jump.Host
	metadata := map[string]string{
		"hostAddress": host,
		"jumpNode":    jump.DisplayString(),
	}
	if len(host) != 0 {
		if host[0] == '-' {
			return message.New(message.EngineTargetConfigInvalidHostFormat).WithMetadata(metadata)
		}
	}

	// Host names cannot start with "-" and cannot contain whitespace, ASCII control characters, or any of the following symbols `'"$\:&<>|(){}
	hostnamePattern := `^[^\x00-\x1F\s'"$\\;&<>|(){}]+$`
	re := regexp.MustCompile(hostnamePattern)

	if re.MatchString(host) {
		return nil
	}

	return message.New(message.EngineTargetConfigInvalidHostFormat).WithMetadata(metadata)
}

func validatePort(jump SSHHostConfig) error {
	port := jump.Port
	metadata := map[string]string{
		"portNum":  strconv.Itoa(int(port)),
		"jumpNode": jump.DisplayString(),
	}
	if port <= 0 {
		return message.New(message.EngineCommonInvalidPortFormat).WithMetadata(metadata)
	}
	return nil
}

func validateKey(jump SSHHostConfig) error {
	// Accept empty private key path as it indicates the user wants to auto-detect keys at connection time.
	if jump.PrivateKeyFilename == "" {
		return nil
	}

	_, err := ssh2.ValidateSSHKey(afero.NewOsFs(), jump.PrivateKeyFilename)

	return err
}

func isNameReserved(name string) bool {
	return name == LocalhostName
}

// TryUnmarshalJSONTargetString tries to decode tgtString as JSON,
// and then converts the JSON to an engine target.
func TryUnmarshalJSONTargetString(tgtString string) (Target, error) {
	var jsonTarget JSONTarget
	var engineTarget Target

	if !util.IsJSON(tgtString) {
		return nil, nil
	}

	err := json.Unmarshal([]byte(tgtString), &jsonTarget)
	if err != nil {
		return nil, message.New(message.EngineTargetConfigInvalidFormat).WithCause(err)
	}

	engineTarget, err = EngineTargetFromJSON(jsonTarget)
	if err != nil {
		return nil, message.New(message.EngineTargetConfigInvalidFormat).WithCause(err)
	}

	return engineTarget, nil
}
