// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type sqlRendererComponentTypeConfig struct {
	Name          string `json:"name"`
	SchemaVersion string `json:"schema_version"`
}

type sqlRendererPortConfig struct {
	Name          string                         `json:"name"`
	Description   string                         `json:"description"`
	Cardinality   string                         `json:"cardinality"`
	ComponentType sqlRendererComponentTypeConfig `json:"component_type"`
}

type sqlRendererConfig struct {
	SQL    string                  `json:"sql"`
	Inputs []sqlRendererPortConfig `json:"inputs"`
	Output *sqlRendererPortConfig  `json:"output"`
}

type SQLRenderer struct {
	config         *render.Config
	specificConfig *sqlRendererConfig
	inputSpec      render.InputSpec
	outputSpec     render.OutputSpec
}

func (renderer *SQLRenderer) Name() string {
	return "SQLRenderer"
}

func (renderer *SQLRenderer) Version() string {
	return "1.0"
}

func (renderer *SQLRenderer) Configure(config *render.Config) error {
	renderer.config = config

	var err error
	renderer.specificConfig, err = util.DecodeJSON[sqlRendererConfig]([]byte(config.JSON))
	if err != nil {
		return message.New(message.EngineRenderSqlRendererConfigInvalidJson).WithCause(err)
	}
	if strings.TrimSpace(renderer.specificConfig.SQL) == "" {
		return message.New(message.EngineRenderSqlRendererSqlRequired)
	}
	if renderer.specificConfig.Output == nil {
		return message.New(message.EngineRenderSqlRendererOutputRequired)
	}

	renderer.inputSpec, err = buildSQLInputSpec(renderer.specificConfig.Inputs)
	if err != nil {
		return err
	}
	renderer.outputSpec, err = buildSQLOutputSpec(renderer.specificConfig.Output)
	if err != nil {
		return err
	}

	return nil
}

func buildSQLInputSpec(ports []sqlRendererPortConfig) (render.InputSpec, error) {
	result := render.InputSpec{}
	for _, port := range ports {
		spec, err := buildSQLPortSpec(port)
		if err != nil {
			return render.InputSpec{}, err
		}
		result.Ports = append(result.Ports, spec)
	}
	return result, nil
}

func buildSQLOutputSpec(port *sqlRendererPortConfig) (render.OutputSpec, error) {
	spec, err := buildSQLPortSpec(*port)
	if err != nil {
		return render.OutputSpec{}, err
	}
	if spec.Cardinality != render.CardinalityOne {
		return render.OutputSpec{}, message.New(message.EngineRenderSqlRendererOutputCardinalityInvalid).WithMetadata(map[string]string{
			"portName":    port.Name,
			"cardinality": strings.ToLower(strings.TrimSpace(port.Cardinality)),
		})
	}
	return render.OutputSpec{PortList: render.PortList{Ports: []render.PortSpec{spec}}}, nil
}

func buildSQLPortSpec(port sqlRendererPortConfig) (render.PortSpec, error) {
	var cardinality render.Cardinality
	switch strings.ToLower(strings.TrimSpace(port.Cardinality)) {
	case "", "one":
		cardinality = render.CardinalityOne
	case "per_run":
		cardinality = render.CardinalityPerRun
	default:
		return render.PortSpec{}, message.New(message.EngineRenderSqlRendererPortCardinalityUnsupported).WithMetadata(map[string]string{
			"portName":    port.Name,
			"cardinality": port.Cardinality,
		})
	}

	return render.PortSpec{
		Name:        port.Name,
		Description: port.Description,
		Cardinality: cardinality,
		ComponentType: cdf.ComponentType{
			Name:          port.ComponentType.Name,
			SchemaVersion: port.ComponentType.SchemaVersion,
		},
	}, nil
}

func (renderer *SQLRenderer) Initialize(session render.Session, resolvedDataSources map[string][]render.TableRef) error {
	if len(renderer.outputSpec.Ports) != 1 {
		return message.New(message.EngineRenderSqlRendererOutputPortCountInvalid).WithMetadata(map[string]string{
			"count": strconv.Itoa(len(renderer.outputSpec.Ports)),
		})
	}

	resolvedSQL, err := renderer.resolveSQL(session, resolvedDataSources)
	if err != nil {
		return err
	}
	if err := validateSingleSQLStatement(session.Database(), resolvedSQL); err != nil {
		return err
	}

	output := renderer.outputSpec.Ports[0]
	viewName := session.Manifest().AddEntry(render.NewManifestEntryInfo(
		output.ComponentType,
		renderer.config.Identity,
		associatedContentIDs(session.Content()),
	))

	//nolint:gosec
	query := `CREATE OR REPLACE VIEW ` + sqlDoubleQuoteString(viewName) + ` AS ` + resolvedSQL
	if _, err := session.Database().Conn.ExecContext(context.Background(), query); err != nil {
		return message.New(message.EngineRenderSqlRendererViewCreateFailed).WithMetadata(map[string]string{
			"viewName": viewName,
			"portName": output.Name,
		}).WithCause(err)
	}

	return nil
}

func associatedContentIDs(content *render.ContentMap) []run.RunID {
	if content == nil {
		return nil
	}
	result := make([]run.RunID, 0, len(content.Entries))
	for _, entry := range content.Entries {
		result = append(result, entry.ID)
	}
	return result
}

func validateSingleSQLStatement(db *render.Database, query string) error {
	if db == nil || db.Conn == nil {
		return message.New(message.EngineRenderSqlRendererDatabaseConnectionRequired)
	}

	return db.Conn.Raw(func(driverConn any) error {
		conn, ok := driverConn.(driver.Conn)
		if !ok {
			return message.New(message.EngineRenderSqlRendererDatabaseConnectionInvalid).WithMetadata(map[string]string{
				"connectionType": fmt.Sprintf("%T", driverConn),
			})
		}

		rawConn, err := render.GetRawDuckDBConn(conn)
		if err != nil {
			return message.New(message.EngineRenderSqlRendererDatabaseConnectionInvalid).WithMetadata(map[string]string{
				"connectionType": fmt.Sprintf("%T", conn),
			}).WithCause(err)
		}

		stmt, err := rawConn.Prepare(query)
		if err != nil {
			if strings.Contains(err.Error(), "multi-statement query") {
				return message.New(message.EngineRenderSqlRendererSqlMultiStatement).WithCause(err)
			}
			return message.New(message.EngineRenderSqlRendererSqlInvalid).WithCause(err)
		}
		return stmt.Close()
	})
}

func (renderer *SQLRenderer) resolveSQL(session render.Session, resolvedDataSources map[string][]render.TableRef) (string, error) {
	sqlText := renderer.specificConfig.SQL

	for {
		start := strings.Index(sqlText, "{{")
		if start < 0 {
			return sqlText, nil
		}
		end := strings.Index(sqlText[start+2:], "}}")
		if end < 0 {
			return "", message.New(message.EngineRenderSqlRendererPlaceholderUnclosed)
		}
		end += start + 2

		placeholder := sqlText[start+2 : end]
		resolved, err := renderer.resolvePlaceholder(session, resolvedDataSources, placeholder)
		if err != nil {
			return "", err
		}

		sqlText = sqlText[:start] + resolved + sqlText[end+2:]
	}
}

func (renderer *SQLRenderer) resolvePlaceholder(
	session render.Session,
	resolvedDataSources map[string][]render.TableRef,
	placeholder string,
) (string, error) {
	switch {
	case strings.HasPrefix(placeholder, "table:"):
		return resolveTablePlaceholder(resolvedDataSources, strings.TrimPrefix(placeholder, "table:"))
	case strings.HasPrefix(placeholder, "path:"):
		return resolvePathPlaceholder(session, strings.TrimPrefix(placeholder, "path:"))
	default:
		return "", message.New(message.EngineRenderSqlRendererPlaceholderUnsupported).WithMetadata(map[string]string{
			"placeholder": placeholder,
		})
	}
}

func resolveTablePlaceholder(resolvedDataSources map[string][]render.TableRef, body string) (string, error) {
	parts := strings.Split(body, ":")
	if len(parts) > 2 {
		return "", message.New(message.EngineRenderSqlRendererTablePlaceholderFormatInvalid).WithMetadata(map[string]string{
			"placeholder": body,
		})
	}
	if len(parts) == 0 || parts[0] == "" {
		return "", message.New(message.EngineRenderSqlRendererTablePlaceholderNameRequired)
	}

	name := parts[0]
	tables, ok := resolvedDataSources[name]
	if !ok {
		return "", message.New(message.EngineRenderSqlRendererTablePlaceholderUnbound).WithMetadata(map[string]string{
			"placeholderName": name,
		})
	}

	index := 0
	if len(parts) > 1 {
		parsed, err := strconv.Atoi(parts[1])
		if err != nil {
			return "", message.New(message.EngineRenderSqlRendererTablePlaceholderIndexInvalid).WithMetadata(map[string]string{
				"placeholderName": name,
				"index":           parts[1],
			}).WithCause(err)
		}
		index = parsed
	} else if len(tables) != 1 {
		return "", message.New(message.EngineRenderSqlRendererTablePlaceholderAmbiguous).WithMetadata(map[string]string{
			"placeholderName": name,
		})
	}

	if index < 0 || index >= len(tables) {
		return "", message.New(message.EngineRenderSqlRendererTablePlaceholderIndexOutOfRange).WithMetadata(map[string]string{
			"placeholderName": name,
			"index":           strconv.Itoa(index),
		})
	}

	return sqlDoubleQuoteString(tables[index].Name), nil
}

func resolvePathPlaceholder(session render.Session, body string) (string, error) {
	contentIndex := 0
	relativePath := body

	parts := strings.Split(body, ":")
	if len(parts) > 2 {
		return "", message.New(message.EngineRenderSqlRendererPathPlaceholderFormatInvalid).WithMetadata(map[string]string{
			"placeholder": body,
		})
	}
	if len(parts) == 2 {
		if parsed, err := strconv.Atoi(parts[0]); err == nil {
			contentIndex = parsed
			relativePath = parts[1]
		}
	}

	if relativePath == "" {
		return "", message.New(message.EngineRenderSqlRendererPathPlaceholderPathRequired)
	}
	if contentIndex < 0 || contentIndex >= len(session.Content().Entries) {
		return "", message.New(message.EngineRenderSqlRendererPathPlaceholderContentIndexOutOfRange).WithMetadata(map[string]string{
			"index": strconv.Itoa(contentIndex),
			"count": strconv.Itoa(len(session.Content().Entries)),
		})
	}

	cleanRelativePath := filepath.Clean(filepath.FromSlash(relativePath))
	component, err := session.Content().Entries[contentIndex].Model.ResolveComponentByManifestPattern(cleanRelativePath)
	if err != nil {
		return "", message.New(message.EngineRenderSqlRendererPathPlaceholderComponentNotFound).WithMetadata(map[string]string{
			"path": relativePath,
		}).WithCause(err)
	}
	return util.SQLQuoteStringLiteral(component.AbsolutePath), nil
}

func (renderer *SQLRenderer) GetInputSpec() render.InputSpec {
	return renderer.inputSpec
}

func (renderer *SQLRenderer) GetOutputSpec() render.OutputSpec {
	return renderer.outputSpec
}
