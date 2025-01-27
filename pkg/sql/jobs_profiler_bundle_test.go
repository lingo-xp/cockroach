// Copyright 2023 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package sql_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime/pprof"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/cockroach/pkg/base"
	"github.com/cockroachdb/cockroach/pkg/jobs"
	"github.com/cockroachdb/cockroach/pkg/jobs/jobspb"
	"github.com/cockroachdb/cockroach/pkg/jobs/jobsprofiler"
	"github.com/cockroachdb/cockroach/pkg/server/serverpb"
	"github.com/cockroachdb/cockroach/pkg/settings/cluster"
	"github.com/cockroachdb/cockroach/pkg/sql"
	"github.com/cockroachdb/cockroach/pkg/sql/isql"
	"github.com/cockroachdb/cockroach/pkg/sql/physicalplan"
	"github.com/cockroachdb/cockroach/pkg/sql/tests"
	"github.com/cockroachdb/cockroach/pkg/testutils/jobutils"
	"github.com/cockroachdb/cockroach/pkg/testutils/serverutils"
	"github.com/cockroachdb/cockroach/pkg/testutils/sqlutils"
	"github.com/cockroachdb/cockroach/pkg/util/httputil"
	"github.com/cockroachdb/cockroach/pkg/util/leaktest"
	"github.com/cockroachdb/cockroach/pkg/util/log"
	"github.com/cockroachdb/cockroach/pkg/util/protoutil"
	"github.com/cockroachdb/cockroach/pkg/util/uuid"
	"github.com/stretchr/testify/require"
)

// TestReadWriteProfilerExecutionDetails is an end-to-end test of requesting and collecting
// execution details for a job.
func TestReadWriteProfilerExecutionDetails(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)

	// Timeout the test in a few minutes if it hasn't succeeded.
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Minute*2)
	defer cancel()

	params, _ := tests.CreateTestServerParams()
	params.Knobs.JobsTestingKnobs = jobs.NewTestingKnobsWithShortIntervals()
	defer jobs.ResetConstructors()()
	s, sqlDB, _ := serverutils.StartServer(t, params)
	defer s.Stopper().Stop(ctx)

	runner := sqlutils.MakeSQLRunner(sqlDB)

	runner.Exec(t, `CREATE TABLE t (id INT)`)
	runner.Exec(t, `INSERT INTO t SELECT generate_series(1, 100)`)

	t.Run("read/write DistSQL diagram", func(t *testing.T) {
		jobs.RegisterConstructor(jobspb.TypeImport, func(j *jobs.Job, _ *cluster.Settings) jobs.Resumer {
			return fakeExecResumer{
				OnResume: func(ctx context.Context) error {
					p := sql.PhysicalPlan{}
					infra := physicalplan.NewPhysicalInfrastructure(uuid.FastMakeV4(), base.SQLInstanceID(1))
					p.PhysicalInfrastructure = infra
					jobsprofiler.StorePlanDiagram(ctx, s.Stopper(), &p, s.InternalDB().(isql.DB), j.ID())
					checkForPlanDiagrams(ctx, t, s.InternalDB().(isql.DB), j.ID(), 1)
					return nil
				},
			}
		}, jobs.UsesTenantCostControl)

		var importJobID int
		runner.QueryRow(t, `IMPORT INTO t CSV DATA ('nodelocal://1/foo') WITH DETACHED`).Scan(&importJobID)
		jobutils.WaitForJobToSucceed(t, runner, jobspb.JobID(importJobID))

		runner.Exec(t, `SELECT crdb_internal.request_job_execution_details($1)`, importJobID)
		distSQLDiagram := checkExecutionDetails(t, s, jobspb.JobID(importJobID), "distsql")
		require.Regexp(t, "<meta http-equiv=\"Refresh\" content=\"0\\; url=https://cockroachdb\\.github\\.io/distsqlplan/decode.html.*>", string(distSQLDiagram))
	})

	t.Run("read/write goroutines", func(t *testing.T) {
		blockCh := make(chan struct{})
		continueCh := make(chan struct{})
		defer close(blockCh)
		defer close(continueCh)
		jobs.RegisterConstructor(jobspb.TypeImport, func(j *jobs.Job, _ *cluster.Settings) jobs.Resumer {
			return fakeExecResumer{
				OnResume: func(ctx context.Context) error {
					pprof.Do(ctx, pprof.Labels("foo", "bar"), func(ctx2 context.Context) {
						blockCh <- struct{}{}
						<-continueCh
					})
					return nil
				},
			}
		}, jobs.UsesTenantCostControl)
		var importJobID int
		runner.QueryRow(t, `IMPORT INTO t CSV DATA ('nodelocal://1/foo') WITH DETACHED`).Scan(&importJobID)
		<-blockCh
		runner.Exec(t, `SELECT crdb_internal.request_job_execution_details($1)`, importJobID)
		goroutines := checkExecutionDetails(t, s, jobspb.JobID(importJobID), "goroutines")
		continueCh <- struct{}{}
		jobutils.WaitForJobToSucceed(t, runner, jobspb.JobID(importJobID))
		require.True(t, strings.Contains(string(goroutines), fmt.Sprintf("labels: {\"foo\":\"bar\", \"job\":\"IMPORT id=%d\", \"n\":\"1\"}", importJobID)))
		require.True(t, strings.Contains(string(goroutines), "github.com/cockroachdb/cockroach/pkg/sql_test.fakeExecResumer.Resume"))
	})
}

func TestListProfilerExecutionDetails(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)

	// Timeout the test in a few minutes if it hasn't succeeded.
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Minute*2)
	defer cancel()

	params, _ := tests.CreateTestServerParams()
	params.Knobs.JobsTestingKnobs = jobs.NewTestingKnobsWithShortIntervals()
	defer jobs.ResetConstructors()()
	s, sqlDB, _ := serverutils.StartServer(t, params)
	defer s.Stopper().Stop(ctx)

	runner := sqlutils.MakeSQLRunner(sqlDB)
	execCfg := s.ExecutorConfig().(sql.ExecutorConfig)

	expectedDiagrams := 1
	jobs.RegisterConstructor(jobspb.TypeImport, func(j *jobs.Job, _ *cluster.Settings) jobs.Resumer {
		return fakeExecResumer{
			OnResume: func(ctx context.Context) error {
				p := sql.PhysicalPlan{}
				infra := physicalplan.NewPhysicalInfrastructure(uuid.FastMakeV4(), base.SQLInstanceID(1))
				p.PhysicalInfrastructure = infra
				jobsprofiler.StorePlanDiagram(ctx, s.Stopper(), &p, s.InternalDB().(isql.DB), j.ID())
				checkForPlanDiagrams(ctx, t, s.InternalDB().(isql.DB), j.ID(), expectedDiagrams)
				if err := execCfg.JobRegistry.CheckPausepoint("fakeresumer.pause"); err != nil {
					return err
				}
				return nil
			},
		}
	}, jobs.UsesTenantCostControl)

	runner.Exec(t, `CREATE TABLE t (id INT)`)
	runner.Exec(t, `INSERT INTO t SELECT generate_series(1, 100)`)

	t.Run("list execution detail files", func(t *testing.T) {
		runner.Exec(t, `SET CLUSTER SETTING jobs.debug.pausepoints = 'fakeresumer.pause'`)
		var importJobID int
		runner.QueryRow(t, `IMPORT INTO t CSV DATA ('nodelocal://1/foo') WITH DETACHED`).Scan(&importJobID)
		jobutils.WaitForJobToPause(t, runner, jobspb.JobID(importJobID))

		runner.Exec(t, `SELECT crdb_internal.request_job_execution_details($1)`, importJobID)
		files := listExecutionDetails(t, s, jobspb.JobID(importJobID))
		require.Len(t, files, 2)
		require.Regexp(t, "distsql\\..*\\.html", files[0])
		require.Regexp(t, "goroutines\\..*\\.txt", files[1])

		// Resume the job, so it can write another DistSQL diagram and goroutine
		// snapshot.
		runner.Exec(t, `SET CLUSTER SETTING jobs.debug.pausepoints = ''`)
		expectedDiagrams = 2
		runner.Exec(t, `RESUME JOB $1`, importJobID)
		jobutils.WaitForJobToSucceed(t, runner, jobspb.JobID(importJobID))
		runner.Exec(t, `SELECT crdb_internal.request_job_execution_details($1)`, importJobID)
		files = listExecutionDetails(t, s, jobspb.JobID(importJobID))
		require.Len(t, files, 4)
		require.Regexp(t, "distsql\\..*\\.html", files[0])
		require.Regexp(t, "distsql\\..*\\.html", files[1])
		require.Regexp(t, "goroutines\\..*\\.txt", files[2])
		require.Regexp(t, "goroutines\\..*\\.txt", files[3])
	})
}

func listExecutionDetails(
	t *testing.T, s serverutils.TestServerInterface, jobID jobspb.JobID,
) []string {
	t.Helper()

	client, err := s.GetAdminHTTPClient()
	require.NoError(t, err)

	url := s.AdminURL().String() + fmt.Sprintf("/_status/list_job_profiler_execution_details/%d", jobID)
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)

	req.Header.Set("Content-Type", httputil.ProtoContentType)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	edResp := serverpb.ListJobProfilerExecutionDetailsResponse{}
	require.NoError(t, protoutil.Unmarshal(body, &edResp))
	sort.Slice(edResp.Files, func(i, j int) bool {
		return edResp.Files[i] < edResp.Files[j]
	})
	return edResp.Files
}

func checkExecutionDetails(
	t *testing.T, s serverutils.TestServerInterface, jobID jobspb.JobID, filename string,
) []byte {
	t.Helper()

	client, err := s.GetAdminHTTPClient()
	require.NoError(t, err)

	url := s.AdminURL().String() + fmt.Sprintf("/_status/job_profiler_execution_details/%d/%s", jobID, filename)
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)

	req.Header.Set("Content-Type", httputil.ProtoContentType)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	edResp := serverpb.GetJobProfilerExecutionDetailResponse{}
	require.NoError(t, protoutil.Unmarshal(body, &edResp))

	r := bytes.NewReader(edResp.Data)
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	return data
}
