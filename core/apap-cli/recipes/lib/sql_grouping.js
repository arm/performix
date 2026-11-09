// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

/**
 * Generate a DuckDB CASE expression using RE2 regular expressions.
 * Patterns search each value cast to VARCHAR; use ^ and $ for boundaries.
 * The first matching group wins. Put specific exceptions before broader rules;
 * patterns within a group are combined with OR. Overlaps are not detected.
 *
 * Column names and string literals are SQL-escaped. Empty pattern lists, empty
 * patterns, and duplicate pattern strings are rejected. Invalid regex syntax
 * fails when DuckDB executes the query. NULL and unmatched values use fallback;
 * an empty groups array returns the fallback literal.
 *
 * @param {object} args
 * @param {string} args.column - Literal column name, not an SQL expression or
 * qualified reference.
 * @param {{label: string, patterns: string[]}[]} args.groups - Ordered category
 * rules. Patterns use RE2 syntax; double backslashes in JavaScript strings.
 * @param {string} [args.fallback='Unclassified'] - Category for unmatched values.
 * @returns {string} SQL expression without an output alias or aggregation.
 * @example
 * // From a recipe file under recipes/:
 * const { makeGroupingSQL } = require('./lib/sql_grouping');
 * const groupExpression = makeGroupingSQL({
 *   column: 'name',
 *   groups: [
 *     { label: 'Group 1', patterns: ['^item1\\.'] },
 *     { label: 'Group 2', patterns: ['^item2\\.'] },
 *   ],
 * });
 * const sql = `SELECT name, ${groupExpression} AS category FROM items`;
 */
function makeGroupingSQL({ column, groups, fallback = 'Unclassified' }) {
  const identifier = '"' + column.replace(/"/g, '""') + '"';
  /** @param {string} value */
  const literal = (value) => "'" + value.replace(/'/g, "''") + "'";
  const seen = new Set();
  const clauses = groups.map(({ label, patterns }) => {
    if (patterns.length === 0) {
      throw new Error(`No patterns for group: ${label}`);
    }
    const conditions = patterns.map((pattern) => {
      if (pattern.length === 0) {
        throw new Error(`Empty grouping pattern for: ${label}`);
      }
      if (seen.has(pattern)) {
        throw new Error(`Duplicate grouping pattern: ${pattern}`);
      }
      seen.add(pattern);
      return `regexp_matches(CAST(${identifier} AS VARCHAR), ${literal(pattern)})`;
    });
    return `WHEN (${conditions.join(' OR ')}) THEN ${literal(label)}`;
  });
  return clauses.length
    ? `CASE\n${clauses.join('\n')}\nELSE ${literal(fallback)} END`
    : literal(fallback);
}

module.exports = { makeGroupingSQL };
