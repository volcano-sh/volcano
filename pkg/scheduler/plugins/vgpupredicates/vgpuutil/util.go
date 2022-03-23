package vgpuutil

import (
	"fmt"
	"strings"
	"time"
)

func escapeJSONPointer(p string) string {
	// Escaping reference name using https://tools.ietf.org/html/rfc6901
	p = strings.Replace(p, "~", "~0", -1)
	p = strings.Replace(p, "/", "~1", -1)
	return p
}

func arrayencode(ids []int) string {
	res := ""
	for _, val := range ids {
		if len(res) == 0 {
			res = fmt.Sprintf("%d", val)
		} else {
			res = fmt.Sprintf("%s,%d", res, val)
		}
	}
	return res
}

// AddGPUIndexPatch returns the patch adding GPU index
func AddGPUIndexPatch(ids []int) string {
	return fmt.Sprintf(`[{"op": "add", "path": "/metadata/annotations/%s", "value":"%d"},`+
		`{"op": "add", "path": "/metadata/annotations/%s", "value": "%s"}]`,
		escapeJSONPointer(PredicateTime), time.Now().UnixNano(),
		escapeJSONPointer(GPUIndex), arrayencode(ids))
}
