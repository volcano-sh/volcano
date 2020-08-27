/*
Copyright 2020 The Volcano Authors.

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

package filewatcher

import "github.com/fsnotify/fsnotify"

type FileWatcher interface {
	Events() chan fsnotify.Event
	Errors() chan error
	Close()
}

type fileWatcher struct {
	watcher *fsnotify.Watcher
}

func NewFileWatcher(path string) (FileWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	err = watcher.Add(path)
	if err != nil {
		return nil, err
	}

	return &fileWatcher{
		watcher: watcher,
	}, nil

}

func (w *fileWatcher) Events() chan fsnotify.Event {
	if w == nil || w.watcher == nil {
		return nil
	}
	return w.watcher.Events
}

func (w *fileWatcher) Errors() chan error {
	if w == nil || w.watcher == nil {
		return nil
	}
	return w.watcher.Errors
}

func (w *fileWatcher) Close() {
	if w == nil || w.watcher == nil {
		return
	}
	w.watcher.Close()
}
