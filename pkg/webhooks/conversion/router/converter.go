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

package router

import (
	"fmt"
	"net/http"
	"sync"
)

var conversionMap = make(map[string]*ConversionService)
var conversionMapLock sync.Mutex

func RegisterConversion(service *ConversionService) error {
	conversionMapLock.Lock()
	defer conversionMapLock.Unlock()

	if _, found := conversionMap[service.Path]; found {
		return fmt.Errorf("duplicated admission service for %s", service.Path)
	}

	// Also register handler to the service.
	service.Handler = func(w http.ResponseWriter, r *http.Request) {
		serve(w, r, service.Func)
	}

	conversionMap[service.Path] = service

	return nil
}

// ForEachConversion handles all conversions.
func ForEachConversion(handler func(*ConversionService)) {
	for _, f := range conversionMap {
		handler(f)
	}
}
