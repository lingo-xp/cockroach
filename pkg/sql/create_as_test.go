// Copyright 2023 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package sql

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cockroachdb/cockroach/pkg/base"
	"github.com/cockroachdb/cockroach/pkg/sql/parser"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree"
	"github.com/cockroachdb/cockroach/pkg/sql/types"
	"github.com/cockroachdb/cockroach/pkg/testutils/serverutils"
	"github.com/cockroachdb/cockroach/pkg/testutils/sqlutils"
	"github.com/cockroachdb/cockroach/pkg/util/leaktest"
	"github.com/stretchr/testify/require"
)

// TestCreateAsVTable verifies that all vtables can be used as the source of
// CREATE TABLE AS and CREATE MATERIALIZED VIEW AS.
func TestCreateAsVTable(t *testing.T) {
	defer leaktest.AfterTest(t)()

	ctx := context.Background()
	testCluster := serverutils.StartNewTestCluster(t, 1, base.TestClusterArgs{})
	defer testCluster.Stopper().Stop(ctx)
	sqlRunner := sqlutils.MakeSQLRunner(testCluster.ServerConn(0))
	var p parser.Parser

	i := 0
	for _, vSchema := range virtualSchemas {
		for _, vSchemaDef := range vSchema.tableDefs {
			if vSchemaDef.isUnimplemented() {
				continue
			}

			var name tree.TableName
			var ctasColumns []string
			schema := vSchemaDef.getSchema()
			statements, err := p.Parse(schema)
			require.NoErrorf(t, err, schema)
			require.Lenf(t, statements, 1, schema)
			switch stmt := statements[0].AST.(type) {
			case *tree.CreateTable:
				name = stmt.Table
				for _, def := range stmt.Defs {
					if colDef, ok := def.(*tree.ColumnTableDef); ok {
						if colDef.Hidden {
							continue
						}
						// Filter out vector columns to prevent error in CTAS:
						// "VECTOR column types are unsupported".
						if colDef.Type == types.Int2Vector || colDef.Type == types.OidVector {
							continue
						}
						ctasColumns = append(ctasColumns, colDef.Name.String())
					}
				}
			case *tree.CreateView:
				name = stmt.Name
				ctasColumns = []string{"*"}
			default:
				require.Failf(t, "missing case", "unexpected type %T for schema %s", stmt, schema)
			}

			fqName := name.FQString()
			// Filter by trace_id to prevent error when selecting from
			// crdb_internal.cluster_inflight_traces:
			// "pq: a trace_id value needs to be specified".
			var where string
			if fqName == `"".crdb_internal.cluster_inflight_traces` {
				where = " WHERE trace_id = 1"
			}

			createTableStmt := fmt.Sprintf(
				"CREATE TABLE test_table_%d AS SELECT %s FROM %s%s",
				i, strings.Join(ctasColumns, ", "), fqName, where,
			)
			sqlRunner.Exec(t, createTableStmt)
			createViewStmt := fmt.Sprintf(
				"CREATE MATERIALIZED VIEW test_view_%d AS SELECT * FROM %s%s",
				i, fqName, where,
			)
			sqlRunner.Exec(t, createViewStmt)
			i++
		}
	}

	waitForJobsSuccess(t, sqlRunner)
}

func TestCreateAsShow(t *testing.T) {
	defer leaktest.AfterTest(t)()

	testCases := []struct {
		sql   string
		setup string
		skip  bool
	}{
		{
			sql: "SHOW CLUSTER SETTINGS",
		},
		{
			sql:   "SHOW CLUSTER SETTINGS FOR TENANT [2]",
			setup: "SELECT crdb_internal.create_tenant(2)",
		},
		{
			sql: "SHOW DATABASES",
		},
		{
			sql:   "SHOW ENUMS",
			setup: "CREATE TYPE e AS ENUM ('a', 'b')",
		},
		{
			sql:   "SHOW TYPES",
			setup: "CREATE TYPE p AS (x int, y int)",
		},
		{
			sql: "SHOW CREATE DATABASE defaultdb",
		},
		{
			sql: "SHOW CREATE ALL SCHEMAS",
		},
		{
			sql: "SHOW CREATE ALL TABLES",
		},
		{
			sql:   "SHOW CREATE TABLE show_create_tbl",
			setup: "CREATE TABLE show_create_tbl (id int PRIMARY KEY)",
			// TODO(sql-foundations): Fix `relation "show_create_tbl" does not exist` error in job.
			//  See https://github.com/cockroachdb/cockroach/issues/106260.
			skip: true,
		},
		{
			sql:   "SHOW CREATE FUNCTION show_create_fn",
			setup: "CREATE FUNCTION show_create_fn(i int) RETURNS INT AS 'SELECT i' LANGUAGE SQL",
			// TODO(sql-foundations): Fix `unknown function: show_create_fn(): function undefined` error in job.
			//  See https://github.com/cockroachdb/cockroach/issues/106268.
			skip: true,
		},
		{
			sql: "SHOW CREATE ALL TYPES",
		},
		{
			sql: "SHOW INDEXES FROM DATABASE defaultdb",
		},
		{
			sql:   "SHOW INDEXES FROM show_indexes_tbl",
			setup: "CREATE TABLE show_indexes_tbl (id int PRIMARY KEY)",
			// TODO(sql-foundations): Fix `relation "show_indexes_tbl" does not exist` error in job.
			//  See https://github.com/cockroachdb/cockroach/issues/106260.
			skip: true,
		},
		{
			sql:   "SHOW COLUMNS FROM show_columns_tbl",
			setup: "CREATE TABLE show_columns_tbl (id int PRIMARY KEY)",
			// TODO(sql-foundations): Fix `relation "show_columns_tbl" does not exist` error in job.
			//  See https://github.com/cockroachdb/cockroach/issues/106260.
			skip: true,
		},
		{
			sql:   "SHOW CONSTRAINTS FROM show_constraints_tbl",
			setup: "CREATE TABLE show_constraints_tbl (id int PRIMARY KEY)",
			// TODO(sql-foundations): Fix `relation "show_constraints_tbl" does not exist` error in job.
			//  See https://github.com/cockroachdb/cockroach/issues/106260.
			skip: true,
		},
		{
			sql: "SHOW PARTITIONS FROM DATABASE defaultdb",
		},
		{
			sql:   "SHOW PARTITIONS FROM TABLE show_partitions_tbl",
			setup: "CREATE TABLE show_partitions_tbl (id int PRIMARY KEY)",
			// TODO(sql-foundations): Fix `relation "show_partitions_tbl" does not exist` error in job.
			//  See https://github.com/cockroachdb/cockroach/issues/106260.
			skip: true,
		},
		{
			sql:   "SHOW PARTITIONS FROM INDEX show_partitions_idx_tbl@show_partitions_idx_tbl_pkey",
			setup: "CREATE TABLE show_partitions_idx_tbl (id int PRIMARY KEY)",
			// TODO(sql-foundations): Fix `relation "show_partitions_idx_tbl" does not exist` error in job.
			//  See https://github.com/cockroachdb/cockroach/issues/106260.
			skip: true,
		},
		{
			sql: "SHOW GRANTS",
		},
		{
			sql: "SHOW JOBS",
		},
		{
			sql: "SHOW CHANGEFEED JOBS",
		},
		{
			sql: "SHOW ALL CLUSTER STATEMENTS",
		},
		{
			sql: "SHOW ALL LOCAL STATEMENTS",
		},
		{
			sql: "SHOW ALL LOCAL STATEMENTS",
		},
		{
			sql: "SHOW RANGES WITH DETAILS, KEYS, TABLES",
		},
		{
			sql:   "SHOW RANGE FROM TABLE show_ranges_tbl FOR ROW (0)",
			setup: "CREATE TABLE show_ranges_tbl (id int PRIMARY KEY)",
			// TODO(sql-foundations): Fix `invalid memory address or nil pointer dereference` error in job.
			//  See https://github.com/cockroachdb/cockroach/issues/106397.
			skip: true,
		},
		{
			sql: "SHOW SURVIVAL GOAL FROM DATABASE",
		},
		{
			sql: "SHOW REGIONS FROM DATABASE",
		},
		{
			sql: "SHOW GRANTS ON ROLE",
		},
		{
			sql: "SHOW ROLES",
		},
		{
			sql: "SHOW SCHEMAS",
		},
		{
			sql:   "SHOW SEQUENCES",
			setup: "CREATE SEQUENCE seq",
		},
		{
			sql: "SHOW ALL SESSIONS",
		},
		{
			sql: "SHOW CLUSTER SESSIONS",
		},
		{
			sql: "SHOW SYNTAX 'SELECT 1'",
		},
		{
			sql:   "SHOW FUNCTIONS",
			setup: "CREATE FUNCTION show_functions_fn(i int) RETURNS INT AS 'SELECT i' LANGUAGE SQL",
		},
		{
			sql: "SHOW TABLES",
		},
		{
			sql: "SHOW ALL TRANSACTIONS",
		},
		{
			sql: "SHOW CLUSTER TRANSACTIONS",
		},
		{
			sql: "SHOW USERS",
		},
		{
			sql: "SHOW ALL",
		},
		{
			sql: "SHOW ZONE CONFIGURATIONS",
		},
		{
			sql: "SHOW SCHEDULES",
		},
		{
			sql: "SHOW JOBS FOR SCHEDULES SELECT id FROM [SHOW SCHEDULES]",
		},
		{
			sql: "SHOW FULL TABLE SCANS",
		},
		{
			sql: "SHOW DEFAULT PRIVILEGES",
		},
	}

	ctx := context.Background()
	testCluster := serverutils.StartNewTestCluster(t, 1, base.TestClusterArgs{})
	defer testCluster.Stopper().Stop(ctx)
	sqlRunner := sqlutils.MakeSQLRunner(testCluster.ServerConn(0))

	for i, testCase := range testCases {
		t.Run(testCase.sql, func(t *testing.T) {
			if testCase.skip {
				return
			}
			if testCase.setup != "" {
				sqlRunner.Exec(t, testCase.setup)
			}
			createTableStmt := fmt.Sprintf(
				"CREATE TABLE test_table_%d AS SELECT * FROM [%s]",
				i, testCase.sql,
			)
			sqlRunner.Exec(t, createTableStmt)
			createViewStmt := fmt.Sprintf(
				"CREATE MATERIALIZED VIEW test_view_%d AS SELECT * FROM [%s]",
				i, testCase.sql,
			)
			sqlRunner.Exec(t, createViewStmt)
			i++
		})
	}

	waitForJobsSuccess(t, sqlRunner)
}

func waitForJobsSuccess(t *testing.T, sqlRunner *sqlutils.SQLRunner) {
	query := `SELECT job_id, status, error, description 
FROM [SHOW JOBS] 
WHERE job_type IN ('SCHEMA CHANGE', 'NEW SCHEMA CHANGE')
AND status != 'succeeded'`
	sqlRunner.CheckQueryResultsRetry(t, query, [][]string{})
}
