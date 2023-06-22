// Copyright 2022 The Cockroach Authors.
//
// Licensed as a CockroachDB Enterprise file under the Cockroach Community
// License (the "License"); you may not use this file except in compliance with
// the License. You may obtain a copy of the License at
//
//     https://github.com/cockroachdb/cockroach/blob/master/licenses/CCL.txt

// Code generated by sctestgen, DO NOT EDIT.

package schemachangerccl

import (
	"testing"

	"github.com/cockroachdb/cockroach/pkg/sql/schemachanger/sctest"
	"github.com/cockroachdb/cockroach/pkg/util/leaktest"
	"github.com/cockroachdb/cockroach/pkg/util/log"
)

func TestBackupMixedVersionElements_ccl_create_index(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)
	sctest.BackupMixedVersionElements(t, "pkg/ccl/schemachangerccl/testdata/end_to_end/create_index", newMultiRegionMixedCluster)
}
func TestBackupMixedVersionElements_ccl_drop_database_multiregion_primary_region(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)
	sctest.BackupMixedVersionElements(t, "pkg/ccl/schemachangerccl/testdata/end_to_end/drop_database_multiregion_primary_region", newMultiRegionMixedCluster)
}
func TestBackupMixedVersionElements_ccl_drop_table_multiregion(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)
	sctest.BackupMixedVersionElements(t, "pkg/ccl/schemachangerccl/testdata/end_to_end/drop_table_multiregion", newMultiRegionMixedCluster)
}
func TestBackupMixedVersionElements_ccl_drop_table_multiregion_primary_region(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)
	sctest.BackupMixedVersionElements(t, "pkg/ccl/schemachangerccl/testdata/end_to_end/drop_table_multiregion_primary_region", newMultiRegionMixedCluster)
}