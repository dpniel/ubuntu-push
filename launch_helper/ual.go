/*
 Copyright 2013-2014 Canonical Ltd.

 This program is free software: you can redistribute it and/or modify it
 under the terms of the GNU General Public License version 3, as published
 by the Free Software Foundation.

 This program is distributed in the hope that it will be useful, but
 WITHOUT ANY WARRANTY; without even the implied warranties of
 MERCHANTABILITY, SATISFACTORY QUALITY, or FITNESS FOR A PARTICULAR
 PURPOSE.  See the GNU General Public License for more details.

 You should have received a copy of the GNU General Public License along
 with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package launch_helper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sync"
	"time"

	"launchpad.net/go-xdg/v0"

	"launchpad.net/ubuntu-push/click"
	"launchpad.net/ubuntu-push/logger"
)

var (
	ErrCantFindHelper = errors.New("can't find helper")
)

type HelperArgs struct {
	Input      *HelperInput
	AppId      string
	FileIn     string
	FileOut    string
	Id         string
	Timer      *time.Timer
	ForcedStop bool
}

type HelperLauncher interface {
	InstallObserver(done func(string)) error
	RemoveObserver() error
	Launch(appId string, exec string, f1 string, f2 string) (string, error)
	Stop(appId string, instanceId string) error
}


type kindHelperPool struct {
	log        logger.Logger
	chOut      chan *HelperResult
	chIn       chan *HelperInput
	launchers  map[string]HelperLauncher
	lock       sync.Mutex
	hmap       map[string]*HelperArgs
	maxRuntime time.Duration
}

func _helperInfo(app *click.AppId) (string, string) {
	return app.Helper()
}

// HelperInfo is overridable for testing
var HelperInfo func(*click.AppId) (string, string) = _helperInfo

// a HelperPool that delegates to different per kind HelperLaunchers
func NewHelperPool(launchers map[string]HelperLauncher, log logger.Logger) HelperPool {
	return &kindHelperPool{
		log:        log,
		hmap:       make(map[string]*HelperArgs),
		launchers:  launchers,
		maxRuntime: 5 * time.Second,
	}
}

func (pool *kindHelperPool) Start() chan *HelperResult {
	pool.chOut = make(chan *HelperResult)
	pool.chIn = make(chan *HelperInput, InputBufferSize)

	for kind, launcher := range pool.launchers {
		err := launcher.InstallObserver(pool.OneDone)
		if err != nil {
			panic(fmt.Errorf("failed to install helper observer for %s: %v", kind, err))
		}
	}

	// xxx make sure at most X helpers are running
	go func() {
		for i := range pool.chIn {
			if pool.handleOne(i) != nil {
				pool.failOne(i)
			}
		}
	}()

	return pool.chOut
}

func (pool *kindHelperPool) Stop() {
	close(pool.chIn)
	for kind, launcher := range pool.launchers {
		err := launcher.RemoveObserver()
		if err != nil {
			panic(fmt.Errorf("failed to remove helper observer for &s: %v", kind, err))
		}
	}
}

func (pool *kindHelperPool) Run(kind string, input *HelperInput) {
	input.kind = kind
	pool.chIn <- input
}

func (pool *kindHelperPool) failOne(input *HelperInput) {
	pool.log.Errorf("unable to get helper output; putting payload into message")
	pool.chOut <- &HelperResult{HelperOutput: HelperOutput{Message: input.Payload, Notification: nil}, Input: input}
}

func (pool *kindHelperPool) cleanupTempFiles(f1, f2 string) {
	if f1 != "" {
		os.Remove(f1)
	}
	if f2 != "" {
		os.Remove(f2)
	}
}

func (pool *kindHelperPool) handleOne(input *HelperInput) error {
	helperAppId, helperExec := HelperInfo(input.App)
	if helperAppId == "" || helperExec == "" {
		pool.log.Errorf("can't locate helper for app")
		return ErrCantFindHelper
	}
	pool.log.Debugf("using helper %s (exec: %s) for app %s", helperAppId, helperExec, input.App)
	var f1, f2 string
	f1, err := pool.createInputTempFile(input)
	defer func() {
		if err != nil {
			pool.cleanupTempFiles(f1, f2)
		}
	}()
	if err != nil {
		pool.log.Errorf("unable to create input tempfile: %v", err)
		return err
	}
	f2, err = pool.createOutputTempFile(input)
	if err != nil {
		pool.log.Errorf("unable to create output tempfile: %v", err)
		return err
	}

	args := HelperArgs{
		AppId:   helperAppId,
		Input:   input,
		FileIn:  f1,
		FileOut: f2,
	}

	pool.lock.Lock()
	defer pool.lock.Unlock()
	mgr := pool.launchers[input.kind]
	iid, err := mgr.Launch(helperAppId, helperExec, f1, f2)
	if err != nil {
		pool.log.Errorf("unable to launch helper %s: %v", helperAppId, err)
		return err
	}
	args.Id = iid
	args.Timer = time.AfterFunc(pool.maxRuntime, func() {
		pool.peekId(iid, func(a *HelperArgs) {
			a.ForcedStop = true
			err := mgr.Stop(helperAppId, iid)
			if err != nil {
				pool.log.Errorf("unable to forcefully stop helper %s: %v", helperAppId, err)
			}
		})
	})
	pool.hmap[iid] = &args

	return nil
}

func (pool *kindHelperPool) peekId(iid string, cb func(*HelperArgs)) *HelperArgs {
	pool.lock.Lock()
	defer pool.lock.Unlock()
	args, ok := pool.hmap[iid]
	if ok {
		cb(args)
		return args
	}
	return nil
}

func (pool *kindHelperPool) OneDone(iid string) {
	args := pool.peekId(iid, func(a *HelperArgs) {
		a.Timer.Stop()
		// dealt with, remove it
		delete(pool.hmap, iid)
	})
	if args == nil {
		// nothing to do
		return
	}
	defer func() {
		pool.cleanupTempFiles(args.FileIn, args.FileOut)
	}()
	if args.ForcedStop {
		pool.failOne(args.Input)
		return
	}
	payload, err := ioutil.ReadFile(args.FileOut)
	if err != nil {
		pool.log.Errorf("unable to read output from helper: %v", err)
	} else {
		res := &HelperResult{Input: args.Input}
		err = json.Unmarshal(payload, &res.HelperOutput)
		if err != nil {
			pool.log.Debugf("failed to parse HelperOutput from helper output: %v", err)
		} else {
			pool.chOut <- res
		}
	}
	if err != nil {
		pool.failOne(args.Input)
	}
}

func (pool *kindHelperPool) createInputTempFile(input *HelperInput) (string, error) {
	f1, err := getTempFilename(input.App.Package)
	if err != nil {
		return "", err
	}
	return f1, ioutil.WriteFile(f1, input.Payload, os.ModeTemporary)
}

func (pool *kindHelperPool) createOutputTempFile(input *HelperInput) (string, error) {
	return getTempFilename(input.App.Package)
}

// helper helpers:

var xdgCacheHome = xdg.Cache.Home

func _getTempDir(pkgName string) (string, error) {
	tmpDir := path.Join(xdgCacheHome(), pkgName)
	err := os.MkdirAll(tmpDir, 0700)
	return tmpDir, err
}

var getTempDir = _getTempDir

func _getTempFilename(pkgName string) (string, error) {
	tmpDir, err := getTempDir(pkgName)
	if err != nil {
		return "", err
	}
	file, err := ioutil.TempFile(tmpDir, "push-helper")
	if err != nil {
		return "", err
	}
	defer file.Close()
	return file.Name(), nil
}

var getTempFilename = _getTempFilename
