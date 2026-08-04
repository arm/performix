// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

type SymbolMapper func(targetFilePath string) (
	mappedPath sql.NullString,
)

const (
	createSourceFilesTableSQL = `CREATE SEQUENCE seq_%[1]s_id START 1;
CREATE TABLE %[1]s (
	source_file_id INTEGER PRIMARY KEY DEFAULT nextval('seq_%[1]s_id'),
	target_location VARCHAR UNIQUE,
	host_location VARCHAR,
);`
	createImagesTableSQL = `CREATE TABLE %[1]s (
	image_id INTEGER PRIMARY KEY,
	image_name VARCHAR NOT NULL UNIQUE
);`
	createSymbolsTableSQL = `CREATE TABLE %[1]s (
	symbol_id INTEGER PRIMARY KEY,
	name VARCHAR,
	image_id INTEGER,
	source_file_id INTEGER,
	first_source_line INTEGER,
	last_source_line INTEGER
);`
)

// NewSourceMapper returns a SymbolMapper which calls run.SearchSourceFile(hosts, targetFilePath) to find a host path.
//
//	– if ok, returns that host path
//	– else returns an empty NullString
func NewSourceMapper(hosts run.HostSourceCodePath) SymbolMapper {
	return func(targetFilePath string) sql.NullString {
		if len(hosts.Paths) == 0 {
			return sql.NullString{}
		}

		hostFilePath, mapped := run.SearchSourceFile(hosts, targetFilePath)
		if !mapped {
			return sql.NullString{}
		} else {
			return sql.NullString{String: hostFilePath, Valid: true}
		}
	}
}

// LoadSymbolTables loads symbol, images and source_files tables into the database.
//
// It resolves the given symbols JSON and source code capture components from the provided model, and reads the data
// from these into two large SQL views. It creates a table and manifest entry for periodic samples, with the table
// containing the information from all source code attribution CSV files, as well as tables and manifest entries for
// image name-id and source file path-id mappings. It then creates a manifest entry and database table for the symbols,
// and populates it with data from the symbols JSON.
func LoadSymbolTables(
	db *sql.Conn,
	model cdf.ModelView,
	symbolsJSONs []string,
	addManifestEntry func(componentTypeName string) string,
) error {
	var comp cdf.Component
	var schema semver.SemVer
	var errs []error
	var successfulSymbolsJSON string

	// Resolve the symbols.json component
	for _, symbolsJSON := range symbolsJSONs {
		var err error
		comp, schema, err = model.ResolveComponentExpectTypeV(
			symbolsJSON,
			"sl-collect-symbols",
			semver.VersionRange{Min: &semver.SemVer{Major: 1, Minor: 0, Patch: 0}}, // >= 1.0.0
		)
		if err == nil {
			successfulSymbolsJSON = symbolsJSON
			log.Debugf("using symbols JSON from '%v'", symbolsJSON)
			break
		} else {
			errs = append(errs, err)
		}
	}
	if successfulSymbolsJSON == "" {
		if slices.ContainsFunc(errs, func(err error) bool {
			return errors.Is(err, cdf.ErrComponentPending)
		}) {
			return cdf.ErrComponentPending
		}
		return fmt.Errorf("failed to resolve symbols component(s); tried %v, got the following errors: %v", symbolsJSONs, errs)
	}

	// Tabulated handlers: EXACT acceptance of 1.0.0 or 1.1.0 only.
	type handle = func(db *sql.Conn, comp cdf.Component) error

	handleV10x := func(db *sql.Conn, comp cdf.Component) error {
		if err := assertSymbolsFieldsExist(db, comp); err != nil {
			return err
		}
		// creating raw symbols view
		const symbolsViewName = "raw_symbols"
		if err := createRawSymbolsView(db, comp, symbolsViewName); err != nil {
			return fmt.Errorf("failed to create raw symbols view: %v", err)
		}

		mapper := newSourceMapperFromModel(model)

		// create output tables
		sourceFiles := addManifestEntry("source_files")
		if err := createAndPopulateSourceFiles(db, sourceFiles, symbolsViewName, mapper); err != nil {
			return fmt.Errorf("failed to create source files table: %v", err)
		}

		images := addManifestEntry("images")
		if err := createAndPopulateImages(db, images, symbolsViewName); err != nil {
			return fmt.Errorf("failed to create images table: %v", err)
		}

		symbols := addManifestEntry("symbols")
		if err := createAndPopulateSymbols(db, symbols, symbolsViewName, sourceFiles); err != nil {
			return fmt.Errorf("failed to create symbols table: %v", err)
		}

		// drop raw view
		if _, err := db.ExecContext(context.Background(), `DROP VIEW `+symbolsViewName+`;`); err != nil {
			return err
		}
		return nil
	}

	handlers := []struct {
		ranges []semver.VersionRange
		fn     handle
	}{
		{
			// exactly 1.0.0 **and** exactly 1.1.0
			ranges: []semver.VersionRange{
				{Min: &semver.SemVer{Major: 1, Minor: 0, Patch: 0}, Max: &semver.SemVer{Major: 1, Minor: 0, Patch: 1}}, // [1.0.0, 1.0.1)
				{Min: &semver.SemVer{Major: 1, Minor: 1, Patch: 0}, Max: &semver.SemVer{Major: 1, Minor: 1, Patch: 1}}, // [1.1.0, 1.1.1)
			},
			fn: handleV10x,
		},
	}

	for _, h := range handlers {
		for _, r := range h.ranges {
			if semver.InRange(schema, r) {
				return h.fn(db, comp)
			}
		}
	}

	return fmt.Errorf("unsupported schema_version %s for %q; supported: 1.0.0, 1.1.0",
		schema.String(), successfulSymbolsJSON)
}

func LoadSourceCodeAttributionTables(
	db *sql.Conn,
	model cdf.ModelView,
	sourcesCapture string,
	sourceFilesTableName string,
	addManifestEntry func(componentTypeName string) string,
) error {
	// creating single raw samples view
	// Even if component exists in manifest, this doesn't guarantee existence of relevant CSV files
	samplesViewName := "raw_samples"
	sourceCodeComp, err := model.ResolveComponentByManifestPattern(sourcesCapture)
	if errors.Is(err, cdf.ErrComponentPending) {
		return err
	} else if err != nil {
		log.Warnf("component not found for %q: %v", sourcesCapture, err)
	}

	err = createRawSamplesView(db, sourceCodeComp, samplesViewName)
	if err != nil {
		return fmt.Errorf("failed to create raw samples view: %v", err)
	}

	err = updateSourceFiles(db, samplesViewName, sourceFilesTableName)
	if err != nil {
		return fmt.Errorf("failed to update source files table: %v", err)
	}
	if err := mapFilePaths(db, newSourceMapperFromModel(model), sourceFilesTableName); err != nil {
		return fmt.Errorf("failed to map source file paths: %v", err)
	}

	// creating periodic samples table
	periodicSamplesTableName := addManifestEntry("periodic_samples")
	err = createAndPopulatePeriodicSamples(db, periodicSamplesTableName, samplesViewName, sourceFilesTableName)
	if err != nil {
		return fmt.Errorf("failed to create periodic samples table: %v", err)
	}

	// deleting raw view
	_, err = db.ExecContext(context.Background(), fmt.Sprint(`DROP VIEW `, samplesViewName, `;`))
	if err != nil {
		return err
	}

	return nil
}

// newSourceMapperFromModel returns a mapper that resolves target paths against
// the run's host source roots.
func newSourceMapperFromModel(model cdf.ModelView) SymbolMapper {
	sourceComp, err := model.ResolveComponentExpectType(run.SourceCodeFilename, run.SourceCodeCT())
	if err != nil {
		log.Warnf("failed to resolve %q: %v", run.SourceCodeFilename, err)
		return NewSourceMapper(run.HostSourceCodePath{})
	}

	hosts, err := run.ReadHostSourceCodePath(sourceComp.AbsolutePath)
	if err != nil {
		log.Warnf("failed to read %q: %v", run.SourceCodeFilename, err)
		return NewSourceMapper(run.HostSourceCodePath{})
	}
	return NewSourceMapper(*hosts)
}

func createRawSamplesView(db *sql.Conn, sourceCodeComponent cdf.Component, viewName string) error {
	exist, err := doSamplesFilesExist(sourceCodeComponent)
	if err != nil {
		log.Warnf("failed to check for existence of periodic sampling files, creating empty table instead: %v", err)
		return createEmptySamplesView(db, viewName)
	}
	if !exist {
		return createEmptySamplesView(db, viewName)
	}
	// Note that at the moment this ignores extra functions from the first 'original' set, as well as all functions from
	// the second 'inlined into' set
	// union_by_name is used to ensure that a mismatch in the number of columns in different CSVs doesn't cause an error
	// strict_mode is FALSE; this contributes to ignoring extra columns (in combination with union_by_name)
	createViewStatement := fmt.Sprint(
		`CREATE OR REPLACE VIEW `, viewName, ` AS (
		SELECT
			regexp_extract("Functions", '\(([^)]+)\)', 1) AS "image_name",
			"File" AS "source_file_path",
			"Line No" AS "line_no",
			"Inlined" AS "inlined",
			"Periodic Samples" AS "periodic_samples",
			regexp_extract("Functions", '([^(]+)\(', 1) AS "function",
		FROM read_csv(
    		'`, sourceCodeComponent.AbsolutePath, `-*.csv',
    		header        = TRUE,
			null_padding  = TRUE,
			union_by_name = TRUE,
		    strict_mode   = FALSE,
			types = {
				'File':'VARCHAR',
				'Line No':'INTEGER',
				'Inlined':'VARCHAR',
				'Periodic Samples':'INTEGER',
				'Functions':'VARCHAR',
			})
		);`)
	_, err = db.ExecContext(context.Background(), createViewStatement)
	return err
}

// createEmptySamplesView creates a view with the same schema as the expected samples view, but without any data. It is
// necessary in case no periodic sampling CSVs were found, as the read_csv SQL function would fail in such a situation.
func createEmptySamplesView(db *sql.Conn, viewName string) error {
	createViewStatement := fmt.Sprint(
		`CREATE OR REPLACE VIEW `, viewName, ` AS (
		SELECT
			NULL AS "image_name",
			NULL AS "source_file_path",
			NULL AS "line_no",
			NULL AS "inlined",
			NULL AS "periodic_samples",
			NULL AS "function"
		WHERE false
		);`)
	_, err := db.ExecContext(context.Background(), createViewStatement)
	return err
}

func createRawSymbolsView(db *sql.Conn, symbolsComponent cdf.Component, viewName string) error {
	var sourceLineInfoSubStmt string
	// Default to null source line info if not provided
	// Casting is necessary as all source_file_info fields may be null, in which case type isn't inferred correctly
	if doesSymbolsFieldExist(db, symbolsComponent, "source_line_info") {
		sourceLineInfoSubStmt = `
		CAST(source_line_info.source_file_id AS INTEGER) AS source_file_id,
	    CAST(source_line_info.source_file_path AS VARCHAR) AS source_file_path,
	    CAST(source_line_info.first_source_line AS INTEGER) AS first_source_line,
	    CAST(source_line_info.last_source_line AS INTEGER) AS last_source_line
		`
	} else {
		sourceLineInfoSubStmt = `
		CAST(null AS INTEGER) AS source_file_id,
		CAST(null AS VARCHAR) AS source_file_path,
		CAST(null AS INTEGER) AS first_source_line,
		CAST(null AS INTEGER) AS last_source_line`
	}
	createViewStatement := fmt.Sprint(
		`CREATE OR REPLACE VIEW `, viewName, ` AS (
  			SELECT
			   id,
			   name,
               image_id,
			   image_name,
			   `, sourceLineInfoSubStmt, `
  			FROM read_json_auto('`, symbolsComponent.AbsolutePath, `')
		);`)
	_, err := db.ExecContext(context.Background(), createViewStatement)
	return err
}

func createAndPopulateSourceFiles(db *sql.Conn, tableName string, symbolsViewName string, mapper SymbolMapper) error {
	// Note that source file id is generated, and not the same as that in symbols.json
	createTableStatement := fmt.Sprintf(createSourceFilesTableSQL, tableName)
	_, err := db.ExecContext(context.Background(), createTableStatement)
	if err != nil {
		return err
	}

	err = updateSourceFiles(db, symbolsViewName, tableName)
	if err != nil {
		return err
	}

	// Attempting to map paths to host
	return mapFilePaths(db, mapper, tableName)
}

func createImages(db *sql.Conn, tableName string) error {
	createTableStatement := fmt.Sprintf(createImagesTableSQL, tableName)
	_, err := db.ExecContext(context.Background(), createTableStatement)
	return err
}

func createAndPopulateImages(db *sql.Conn, tableName string, symbolsViewName string) error {
	createTableStatement := fmt.Sprintf(createImagesTableSQL, tableName)
	_, err := db.ExecContext(context.Background(), createTableStatement)
	if err != nil {
		return err
	}

	populateStatement := fmt.Sprint(
		`INSERT OR IGNORE INTO `, tableName,
		` SELECT
     		image_id,
			image_name
		FROM `, symbolsViewName, `;`)
	_, err = db.ExecContext(context.Background(), populateStatement)
	return err
}

func createSymbols(db *sql.Conn, tableName string) error {
	createTableStatement := fmt.Sprintf(createSymbolsTableSQL, tableName)
	_, err := db.ExecContext(context.Background(), createTableStatement)
	return err
}

func createAndPopulateSymbols(db *sql.Conn, tableName string, symbolsViewName string, sourceFilesTableName string) error {
	err := createSymbols(db, tableName)
	if err != nil {
		return err
	}

	populateStatement := fmt.Sprint(
		`INSERT INTO `, tableName,
		` SELECT 
			s.id AS symbol_id,
			s.name,
			s.image_id,
			sf.source_file_id,
			s.first_source_line,
			s.last_source_line
		FROM `, symbolsViewName, ` s
		LEFT JOIN `, sourceFilesTableName, ` sf ON s.source_file_path = sf.target_location
		ORDER BY symbol_id;`)
	_, err = db.ExecContext(context.Background(), populateStatement)
	return err
}

func createAndPopulatePeriodicSamples(db *sql.Conn, tableName string, samplesViewName string, sourceFilesTableName string) error {
	createTableStatement := fmt.Sprint(
		`CREATE TABLE `, tableName, ` (
			source_file_id INTEGER NOT NULL,
    		line_no INTEGER NOT NULL,
    		periodic_samples INTEGER NOT NULL,
    		function VARCHAR,
			inlined VARCHAR,
			FOREIGN KEY (source_file_id) REFERENCES `, sourceFilesTableName, `(source_file_id)
		);`)
	_, err := db.ExecContext(context.Background(), createTableStatement)
	if err != nil {
		return err
	}
	populateStatement := fmt.Sprint(
		`INSERT INTO `, tableName,
		` SELECT
			s.source_file_id,
			r.line_no,
			r.periodic_samples,
			r.function,
			r.inlined,
		FROM `, samplesViewName, ` r
		JOIN `, sourceFilesTableName, ` s ON r.source_file_path = s.target_location;`)
	_, err = db.ExecContext(context.Background(), populateStatement)
	return err
}

func mapFilePaths(db *sql.Conn, mapper SymbolMapper, sourceFilesTableName string) error {
	rows, err := db.QueryContext(context.Background(), fmt.Sprint(
		`SELECT
				target_location
			FROM `, sourceFilesTableName, `;`))
	if err != nil {
		return fmt.Errorf("failed to query source file paths: %w", err)
	}
	defer rows.Close()

	// For each source file path, attempt to map it to a location on the host
	for rows.Next() {
		var targetLocation string
		if err := rows.Scan(&targetLocation); err != nil {
			return fmt.Errorf("failed to scan target_location: %w", err)
		}

		hostLocation := mapper(targetLocation)
		if hostLocation.Valid {
			updateEntryStatement := fmt.Sprint(
				`UPDATE `, sourceFilesTableName,
				` SET host_location = '`, hostLocation.String,
				`' WHERE target_location = '`, targetLocation, `';`)
			_, err = db.ExecContext(context.Background(), updateEntryStatement)
			if err != nil {
				return fmt.Errorf("failed to update host_location field: %w", err)
			}
		}
	}
	return nil
}

// assertSymbolsFieldsExist checks for the presence of all required fields in the symbols.json file, and returns an
// 'incompatible schema version' error if such a field is missing
func assertSymbolsFieldsExist(db *sql.Conn, symbolsComponent cdf.Component) error {
	// Note that source_line_info is not included as we can use null for all fields if not provided
	expectedColumns := []string{"id", "name", "image_id", "image_name"}
	for _, columnName := range expectedColumns {
		if !doesSymbolsFieldExist(db, symbolsComponent, columnName) {
			// Note that multiple different schemas were labelled with the same version number 1.0, so this is a white
			// lie; schema version doesn't actually *have* to be 1.1 or above, but if this code path is reached that
			// indicates the component is an 'old' 1.0 version, so we say it should be 1.1 or above to be safe
			return fmt.Errorf("incompatible schema version \"%v\" for component \"%v\"; must be 1.1 or above",
				symbolsComponent.Type.SchemaVersion, symbolsComponent.Type.Name)
		}
	}
	return nil
}

func doesSymbolsFieldExist(db *sql.Conn, symbolsComponent cdf.Component, columnName string) bool {
	createViewStatement := fmt.Sprint(
		`SELECT `, columnName,
		` FROM read_json_auto('`, symbolsComponent.AbsolutePath, `');`)
	_, err := db.ExecContext(context.Background(), createViewStatement)
	return err == nil
}

// doSamplesFilesExist checks for the presence of any periodic sampling csv files using a regex match
func doSamplesFilesExist(sourceCodeComponent cdf.Component) (bool, error) {
	rootDir := filepath.Dir(sourceCodeComponent.AbsolutePath)
	samplesName := filepath.Base(sourceCodeComponent.AbsolutePath)
	regex, err := regexp.Compile(fmt.Sprintf(`%s-.*\.csv$`, regexp.QuoteMeta(samplesName)))
	if err != nil {
		return false, err
	}

	// Walk through files
	found := false
	err = filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if regex.MatchString(d.Name()) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return found, nil
}

// updateSourceFiles adds any new source file paths found in the samples view to the source files table
func updateSourceFiles(db *sql.Conn, samplesViewName, sourceFilesTableName string) error {
	// #nosec G201 -- table/view names come from trusted code paths; values are not interpolated
	stmt := fmt.Sprintf(`
        INSERT OR IGNORE INTO %s (target_location, host_location)
        SELECT s.source_file_path AS target_location, NULL AS host_location
        FROM (
            SELECT DISTINCT source_file_path
            FROM %s
            WHERE source_file_path IS NOT NULL
        ) AS s
        ORDER BY s.source_file_path;`, sourceFilesTableName, samplesViewName)

	_, err := db.ExecContext(context.Background(), stmt)
	return err
}
