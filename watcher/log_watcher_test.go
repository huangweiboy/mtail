// Copyright 2015 Google Inc. All Rights Reserved.
// This file is available under the Apache license.

package watcher

import (
	"errors"
	"expvar"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// This test requires disk access, and cannot be injected without internal
// knowledge of the fsnotify code. Make the wait deadlines long.
const deadline = 5 * time.Second

func TestLogWatcher(t *testing.T) {
	if testing.Short() {
		// This test is slow due to disk access.
		t.Skip("skipping log watcher test in short mode")
	}

	workdir, err := ioutil.TempDir("", "log_watcher_test")
	if err != nil {
		t.Fatalf("could not create temporary working directory: %s", err)
	}

	defer func() {
		if err = os.RemoveAll(workdir); err != nil {
			t.Fatalf("could not remove temp dir %s: %s:", workdir, err)
		}
	}()

	w, err := NewLogWatcher()
	if err != nil {
		t.Fatalf("couldn't create a watcher: %s\n", err)
	}
	defer func() {
		if err = w.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	handle, eventsChannel := w.Events()

	if err = w.Add(workdir, handle); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(workdir, "logfile"))
	if err != nil {
		t.Fatalf("couldn't make a logfile in temp dir: %s\n", err)
	}
	select {
	case e := <-eventsChannel:
		switch e.Op {
		case Create:
			if e.Pathname != filepath.Join(workdir, "logfile") {
				t.Errorf("create doesn't match")
			}
		default:
			t.Errorf("Wrong event type: %q", e)
		}
	case <-time.After(deadline):
		t.Errorf("didn't receive create message before timeout")
	}
	if n, err := f.WriteString("hi"); err != nil {
		t.Fatal(err)
		if n != 2 {
			t.Fatalf("wrote %d instead of 2", n)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-eventsChannel:
		switch e.Op {
		case Update:
			if e.Pathname != filepath.Join(workdir, "logfile") {
				t.Errorf("update doesn't match")
			}
		default:
			t.Errorf("Wrong event type: %q", e)
		}
	case <-time.After(deadline):
		t.Errorf("didn't receive update message before timeout")
	}
	if err := os.Rename(filepath.Join(workdir, "logfile"), filepath.Join(workdir, "logfile2")); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-eventsChannel:
		switch e.Op {
		case Delete:
			if e.Pathname != filepath.Join(workdir, "logfile") {
				t.Errorf("delete doesn't match")
			}
		default:

			t.Errorf("wrong event type: %v", e)
		}
	case <-time.After(deadline):
		t.Errorf("didn't receive delete before timeout")
	}
	select {
	case e := <-eventsChannel:
		switch e.Op {
		case Create:
			if e.Pathname != filepath.Join(workdir, "logfile2") {
				t.Errorf("create doesn't match")
			}
		default:

			t.Errorf("wrong event type: %v", e)
		}
	case <-time.After(deadline):
		t.Errorf("didn't receive create message before timeout")
	}
	if err := os.Chmod(filepath.Join(workdir, "logfile2"), os.ModePerm); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-eventsChannel:
		switch e.Op {
		case Update:
			if e.Pathname != filepath.Join(workdir, "logfile2") {
				t.Errorf("update doesn't match")
			}
		default:

			t.Errorf("wrong event type: %v", e)
		}
	case <-time.After(deadline):
		t.Errorf("didn't receive update message before timeout")
	}
	if err := os.Remove(filepath.Join(workdir, "logfile2")); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-eventsChannel:
		switch e.Op {
		case Delete:
			if e.Pathname != filepath.Join(workdir, "logfile2") {
				t.Errorf("delete doesn't match")
			}
		default:
			t.Errorf("Wrong event type: %q", e)
		}
	case <-time.After(deadline):
		t.Errorf("didn't receive delete message before timeout")
	}
}

// This test may be OS specific; possibly break it out to a file with build tags.
func TestNewLogWatcherError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping log watcher test in short mode")
	}

	var rLimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		t.Fatalf("coulnd't get rlimit: %s", err)
	}
	var zero = rLimit
	zero.Cur = 0
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &zero); err != nil {
		t.Fatalf("couldn't set rlimit: %s", err)
	}
	_, err := NewLogWatcher()
	if err == nil {
		t.Errorf("didn't fail as expected")
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		t.Fatalf("couldn't reset rlimit: %s", err)
	}
}

func TestLogWatcherAddError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping log watcher test in short mode")
	}

	workdir, err := ioutil.TempDir("", "log_watcher_test")
	if err != nil {
		t.Fatalf("could not create temporary working directory: %s", err)
	}

	defer func() {
		err = os.RemoveAll(workdir)
		if err != nil {
			t.Fatalf("could not remove temp dir %s: %s:", workdir, err)
		}
	}()

	w, err := NewLogWatcher()
	if err != nil {
		t.Fatalf("couldn't create a watcher: %s\n", err)
	}
	defer func() {
		if err = w.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	handle, _ := w.Events()
	filename := filepath.Join(workdir, "test")
	err = w.Add(filename, handle)
	if err == nil {
		t.Errorf("did not receive an error for nonexistent file")
	}
}

func TestLogWatcherAddWhilePermissionDenied(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping log watcher test in short mode")
	}
	u, err := user.Current()
	if err != nil {
		t.Skip(fmt.Sprintf("Couldn't determine current user id: %s", err))
	}
	if u.Uid == "0" {
		t.Skip("Skipping test when run as root")
	}

	workdir, err := ioutil.TempDir("", "log_watcher_test")
	if err != nil {
		t.Fatalf("could not create temporary working directory: %s", err)
	}

	defer func() {
		err = os.RemoveAll(workdir)
		if err != nil {
			t.Fatalf("could not remove temp dir %s: %s:", workdir, err)
		}
	}()

	w, err := NewLogWatcher()
	if err != nil {
		t.Fatalf("couldn't create a watcher: %s\n", err)
	}
	defer func() {
		if err = w.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	filename := filepath.Join(workdir, "test")
	if _, err = os.Create(filename); err != nil {
		t.Fatalf("couldn't create file: %s", err)
	}
	if err = os.Chmod(filename, 0); err != nil {
		t.Fatalf("couldn't chmod file: %s", err)
	}
	handle, _ := w.Events()
	err = w.Add(filename, handle)
	if err != nil {
		t.Errorf("failed to add watch on permission denied")
	}
	if err := os.Chmod(filename, 0777); err != nil {
		t.Fatalf("couldn't reset file perms: %s", err)
	}
}

func TestWatcherErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping log watcher test in short mode")
	}
	orig, err := strconv.ParseInt(expvar.Get("log_watcher_error_count").String(), 10, 64)
	if err != nil {
		t.Fatalf("couldn't convert expvar %q", expvar.Get("log_watcher_error_count").String())
	}
	w, err := NewLogWatcher()
	if err != nil {
		t.Fatalf("couldn't create a watcher")
	}
	w.Errors <- errors.New("Injected error for test")
	if err := w.Close(); err != nil {
		t.Fatalf("watcher close failed: %q", err)
	}
	expected := strconv.FormatInt(orig+1, 10)
	if diff := cmp.Diff(expected, expvar.Get("log_watcher_error_count").String()); diff != "" {
		t.Errorf("log watcher error count not increased:\n%s", diff)
	}
}
