// Copyright 2022 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package asim_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/cockroachdb/cockroach/pkg/kv/kvserver/asim"
	"github.com/cockroachdb/cockroach/pkg/kv/kvserver/asim/state"
	"github.com/cockroachdb/cockroach/pkg/kv/kvserver/asim/workload"
	"github.com/stretchr/testify/require"
)

func Example_noWriters() {
	start := state.TestingStartTime()
	s := state.LoadConfig(state.ComplexConfig)
	m := asim.NewMetricsTracker()

	_ = m.Tick(start, s)
	// Output:
}

func Example_tickEmptyState() {
	start := state.TestingStartTime()
	s := state.LoadConfig(state.ComplexConfig)
	m := asim.NewMetricsTracker(os.Stdout)

	_ = m.Tick(start, s)
	// Output:
	//tick,c_ranges,c_write,c_write_b,c_read,c_read_b,s_ranges,s_write,s_write_b,s_read,s_read_b,c_lease_moves,c_replica_moves,c_replica_b_moves
	//2022-03-21 11:00:00 +0000 UTC,1,0,0,0,0,0,0,0,0,1,0,0
}

func TestTickEmptyState(t *testing.T) {
	start := state.TestingStartTime()
	s := state.LoadConfig(state.ComplexConfig)

	var buf bytes.Buffer
	m := asim.NewMetricsTracker(&buf)

	_ = m.Tick(start, s)

	expected :=
		"tick,c_ranges,c_write,c_write_b,c_read,c_read_b,s_ranges,s_write,s_write_b,s_read,s_read_b,c_lease_moves,c_replica_moves,c_replica_b_moves\n" +
			"2022-03-21 11:00:00 +0000 UTC,1,0,0,0,0,0,0,0,0,1,0,0\n"
	require.Equal(t, expected, buf.String())
}

func Example_multipleWriters() {
	start := state.TestingStartTime()
	s := state.LoadConfig(state.ComplexConfig)
	m := asim.NewMetricsTracker(os.Stdout, os.Stdout)

	_ = m.Tick(start, s)
	// Output:
	//tick,c_ranges,c_write,c_write_b,c_read,c_read_b,s_ranges,s_write,s_write_b,s_read,s_read_b,c_lease_moves,c_replica_moves,c_replica_b_moves
	//tick,c_ranges,c_write,c_write_b,c_read,c_read_b,s_ranges,s_write,s_write_b,s_read,s_read_b,c_lease_moves,c_replica_moves,c_replica_b_moves
	//2022-03-21 11:00:00 +0000 UTC,1,0,0,0,0,0,0,0,0,1,0,0
	//2022-03-21 11:00:00 +0000 UTC,1,0,0,0,0,0,0,0,0,1,0,0
}

func Example_leaseTransfer() {
	start := state.TestingStartTime()
	s := state.LoadConfig(state.ComplexConfig)
	m := asim.NewMetricsTracker(os.Stdout)
	s.TransferLease(1, 2)

	_ = m.Tick(start, s)
	// Output:
	//tick,c_ranges,c_write,c_write_b,c_read,c_read_b,s_ranges,s_write,s_write_b,s_read,s_read_b,c_lease_moves,c_replica_moves,c_replica_b_moves
	//2022-03-21 11:00:00 +0000 UTC,1,0,0,0,0,0,0,0,0,2,0,0
}

func Example_rebalance() {
	start := state.TestingStartTime()
	s := state.LoadConfig(state.ComplexConfig)
	m := asim.NewMetricsTracker(os.Stdout)

	// Apply load, to get a replica size greater than 0.
	le := workload.LoadBatch{workload.LoadEvent{Writes: 1, WriteSize: 7, Reads: 2, ReadSize: 9, Key: 5}}
	s.ApplyLoad(le)

	// Do the rebalance.
	c := &state.ReplicaChange{RangeID: 1, Add: 2, Remove: 1}
	c.Apply(s)

	_ = m.Tick(start, s)
	// Output:
	//tick,c_ranges,c_write,c_write_b,c_read,c_read_b,s_ranges,s_write,s_write_b,s_read,s_read_b,c_lease_moves,c_replica_moves,c_replica_b_moves
	//2022-03-21 11:00:00 +0000 UTC,1,3,21,2,9,1,7,2,9,2,1,7
}

func Example_workload() {
	ctx := context.Background()
	start := state.TestingStartTime()
	end := start.Add(200 * time.Second)
	interval := 10 * time.Second
	rwg := make([]workload.Generator, 1)
	rwg[0] = testCreateWorkloadGenerator(start, 10, 10000)
	m := asim.NewMetricsTracker(os.Stdout)

	exchange := state.NewFixedDelayExhange(start, interval, interval)
	changer := state.NewReplicaChanger()
	s := state.LoadConfig(state.ComplexConfig)
	testPreGossipStores(s, exchange, start)

	sim := asim.NewSimulator(start, end, interval, rwg, s, exchange, changer, interval, m)
	sim.RunSim(ctx)
	// Output:
	//tick,c_ranges,c_write,c_write_b,c_read,c_read_b,s_ranges,s_write,s_write_b,s_read,s_read_b,c_lease_moves,c_replica_moves,c_replica_b_moves
	// 2022-03-21 11:00:10 +0000 UTC,1,7500,1430259,47500,9113574,2500,476753,47500,9113574,1,0,0
	// 2022-03-21 11:00:20 +0000 UTC,1,15000,2860140,95000,18230385,5000,953380,95000,18230385,1,0,0
	// 2022-03-21 11:00:30 +0000 UTC,1,22500,4301097,142500,27362846,7500,1433699,142500,27362846,1,0,0
	// 2022-03-21 11:00:40 +0000 UTC,1,30000,5750298,190000,36500898,10000,1916766,190000,36500898,1,0,0
	// 2022-03-21 11:00:50 +0000 UTC,1,37500,7189272,237500,45627899,12500,2396424,237500,45627899,1,0,0
	// 2022-03-21 11:01:00 +0000 UTC,1,45000,8626290,285000,54751653,15000,2875430,285000,54751653,1,0,0
	// 2022-03-21 11:01:10 +0000 UTC,1,52500,10059840,332500,63860672,17500,3353280,332500,63860672,1,0,0
	// 2022-03-21 11:01:20 +0000 UTC,1,60000,11493504,380000,72979157,20000,3831168,380000,72979157,1,0,0
	// 2022-03-21 11:01:30 +0000 UTC,1,67500,12924417,427500,82089114,22500,4308139,427500,82089114,1,0,0
	// 2022-03-21 11:01:40 +0000 UTC,1,75000,14363499,475000,91200047,25000,4787833,475000,91200047,1,0,0
	// 2022-03-21 11:01:50 +0000 UTC,1,82500,15812037,522500,100318896,27500,5270679,522500,100318896,1,0,0
	// 2022-03-21 11:02:00 +0000 UTC,1,90000,17252352,570000,109434086,30000,5750784,570000,109434086,1,0,0
	// 2022-03-21 11:02:10 +0000 UTC,1,97500,18702216,617500,118565208,32500,6234072,617500,118565208,1,0,0
	// 2022-03-21 11:02:20 +0000 UTC,1,105000,20147733,665000,127690714,35000,6715911,665000,127690714,1,0,0
	// 2022-03-21 11:02:30 +0000 UTC,1,112500,21594528,712500,136804862,37500,7198176,712500,136804862,1,0,0
	// 2022-03-21 11:02:40 +0000 UTC,1,120000,23035728,760000,145924346,40000,7678576,760000,145924346,1,0,0
	// 2022-03-21 11:02:50 +0000 UTC,1,127500,24475320,807500,155053079,42500,8158440,807500,155053079,1,0,0
	// 2022-03-21 11:03:00 +0000 UTC,1,135000,25916628,855000,164185683,45000,8638876,855000,164185683,1,0,0
	// 2022-03-21 11:03:10 +0000 UTC,1,142500,27350499,902500,173314547,47500,9116833,902500,173314547,1,0,0
	// 2022-03-21 11:03:20 +0000 UTC,1,150000,28791705,950000,182430770,50000,9597235,950000,182430770,1,0,0
}