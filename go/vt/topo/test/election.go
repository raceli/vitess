// Copyright 2016, Google Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"testing"
	"time"

	"github.com/youtube/vitess/go/vt/topo"
	"golang.org/x/net/context"
)

func waitForMasterID(t *testing.T, mp topo.MasterParticipation, expected string) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		master, err := mp.GetCurrentMasterID()
		if err != nil {
			t.Fatalf("GetCurrentMasterID failed: %v", err)
		}

		if master == expected {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("GetCurrentMasterID timed out with %v, expected %v", master, expected)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// checkElection runs the tests on the MasterParticipation part of the
// topo.Impl API.
func checkElection(t *testing.T, ts topo.Impl) {
	name := "testmp"

	// create a new MasterParticipation
	id1 := "id1"
	mp1, err := ts.NewMasterParticipation(name, id1)
	if err != nil {
		t.Fatalf("cannot create mp1: %v", err)
	}

	// no master yet, check name
	waitForMasterID(t, mp1, "")

	// wait for id1 to be the master
	ctx1, err := mp1.WaitForMastership()
	if err != nil {
		t.Fatalf("mp1 cannot become master: %v", err)
	}

	// get the current master name, better be id1
	waitForMasterID(t, mp1, id1)

	// create a second MasterParticipation on same name
	id2 := "id2"
	mp2, err := ts.NewMasterParticipation(name, id2)
	if err != nil {
		t.Fatalf("cannot create mp2: %v", err)
	}

	// wait until mp2 gets to be the master in the background
	mp2IsMaster := make(chan error)
	var ctx2 context.Context
	go func() {
		var err error
		ctx2, err = mp2.WaitForMastership()
		mp2IsMaster <- err
	}()

	// ask mp2 for master name, should get id1
	waitForMasterID(t, mp2, id1)

	// shutdown mp1
	mp1.Shutdown()

	// this should have closed ctx1 as soon as possible,
	// so 5s will be enough. This will be used during lameduck
	// when the server exits, so we can't wait too long anyway.
	timer := time.NewTimer(5 * time.Second)
	select {
	case <-ctx1.Done():
	case <-timer.C:
		t.Fatalf("shutting down mp1 didn't close ctx1 in time")
	}

	// now mp2 should be master
	err = <-mp2IsMaster
	if err != nil {
		t.Fatalf("mp2 awoke with error: %v", err)
	}

	// ask mp2 for master name, should get id2
	waitForMasterID(t, mp2, id2)

	// shut down mp2, we're done
	mp2.Shutdown()
}
