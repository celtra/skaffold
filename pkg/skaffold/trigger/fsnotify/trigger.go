/*
Copyright 2020 The Skaffold Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package fsnotify

import (
	"context"
	"io"
	"log"
	"time"

	"github.com/dietsche/rfsnotify"
	"github.com/rjeczalik/notify"
	"gopkg.in/fsnotify.v1"

	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/color"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/util"
)

// For testing
var (
	Watch = notify.Watch
)

func New(workspaces map[string]struct{}, isActive func() bool, duration int) *Trigger {
	return &Trigger{
		Interval:   time.Duration(duration) * time.Millisecond,
		workspaces: workspaces,
		isActive:   isActive,
		watchFunc:  Watch,
	}
}

// Trigger watches for changes with fsnotify
type Trigger struct {
	Interval   time.Duration
	workspaces map[string]struct{}
	isActive   func() bool
	watchFunc  func(path string, c chan<- notify.EventInfo, events ...notify.Event) error
}

// IsActive returns the function to run if Trigger is active.
func (t *Trigger) IsActive() func() bool {
	return t.isActive
}

// Debounce tells the watcher to not debounce rapid sequence of changes.
func (t *Trigger) Debounce() bool {
	// This trigger has built-in debouncing.
	return false
}

func (t *Trigger) LogWatchToUser(out io.Writer) {
	if t.isActive() {
		color.Yellow.Fprintln(out, "Watching for changes...")
	} else {
		color.Yellow.Fprintln(out, "Not watching for changes...")
	}
}

// Start listening for file system changes
func (t *Trigger) Start(ctx context.Context) (<-chan bool, error) {
	watcher, err := rfsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	trigger := make(chan bool)
	c := make(chan bool)

	go func() {
		timer := time.NewTimer(1<<63 - 1) // Forever

		log.Printf("%v\n", t.Interval)

		for {
			select {
			case <-c:
				log.Println("c triggered")
				timer.Reset(t.Interval)
			case event, ok := <-watcher.Events:
				log.Printf("some event %v %v\n", ok, event)
				if !ok {
					continue
				}
				log.Println("event:", event)
				if event.Op&fsnotify.Write == fsnotify.Write {
					log.Println("modified file:", event.Name)

					// Wait t.Ienterval before triggering.
					// This way, rapid stream of events will be grouped.
					timer.Reset(t.Interval)
				}
			case err, ok := <-watcher.Errors:
				log.Println("error:", err)
				if !ok {
					return
				}
				log.Println("error:", err)

			case <-timer.C:
				log.Println("timer trigerred")
				trigger <- true

			case <-ctx.Done():
				log.Println("DONE")
				timer.Stop()
				return
			}
		}
	}()

	// Workaround https://github.com/rjeczalik/notify/issues/96
	wd, err := util.RealWorkDir()
	if err != nil {
		return nil, err
	}

	// Watch current directory recursively
	err = watcher.AddRecursive(wd)
	if err != nil {
		log.Println("HERE")
		log.Println(err)
		return nil, err
	}

	// Watch all workspaces recursively
	for w := range t.workspaces {
		if w == "." {
			continue
		}

		if err := watcher.AddRecursive(w); err != nil {
			log.Println("X")
			return nil, err
		}
	}

	// Since the file watcher runs in a separate go routine
	// and can take some time to start, it can lose the very first change.
	// As a mitigation, we act as if a change was detected.
	go func() { c <- true }()

	return trigger, nil
}

// Ignore checks if the change detected is to be ignored or not.
// Currently, returns false i.e Allows all files changed.
func (t *Trigger) Ignore(_ notify.EventInfo) bool {
	return false
}
