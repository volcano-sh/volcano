package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"k8s.io/kubernetes/test/e2e/perftype"
	"math"
	"time"
)

///// PodLatencyData encapsulates pod startup latency information.
type PodLatencyData struct {
	///// Name of the pod
	Name string
	///// Node this pod was running on
	Node string
	///// Latency information related to pod startuptime
	Latency time.Duration
}

type LatencySlice []PodLatencyData

func (a LatencySlice) Len() int           { return len(a) }
func (a LatencySlice) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a LatencySlice) Less(i, j int) bool { return a[i].Latency < a[j].Latency }

func ExtractLatencyMetrics(latencies []PodLatencyData) LatencyMetric {
	length := len(latencies)
	perc50 := latencies[int(math.Ceil(float64(length*50)/100))-1].Latency
	perc90 := latencies[int(math.Ceil(float64(length*90)/100))-1].Latency
	perc99 := latencies[int(math.Ceil(float64(length*99)/100))-1].Latency
	perc100 := latencies[length-1].Latency
	return LatencyMetric{Perc50: perc50, Perc90: perc90, Perc99: perc99, Perc100: perc100}
}

//// Dashboard metrics
type LatencyMetric struct {
	Perc50  time.Duration `json:"Perc50"`
	Perc90  time.Duration `json:"Perc90"`
	Perc99  time.Duration `json:"Perc99"`
	Perc100 time.Duration `json:"Perc100"`
}

type PodStartupLatency struct {
	CreateToScheduleLatency LatencyMetric `json:"createToScheduleLatency"`
	ScheduleToRunLatency    LatencyMetric `json:"scheduleToRunLatency"`
	RunToWatchLatency       LatencyMetric `json:"runToWatchLatency"`
	ScheduleToWatchLatency  LatencyMetric `json:"scheduleToWatchLatency"`
	E2ELatency              LatencyMetric `json:"e2eLatency"`
}

func (l *PodStartupLatency) PrintHumanReadable() string {
	return PrettyPrintJSON(l)
}

func (l *PodStartupLatency) PrintJSON() string {
	return PrettyPrintJSON(PodStartupLatencyToPerfData(l))
}

func PrettyPrintJSON(metrics interface{}) string {
	output := &bytes.Buffer{}
	if err := json.NewEncoder(output).Encode(metrics); err != nil {
		fmt.Println("Error building encoder:", err)
		return ""
	}
	formatted := &bytes.Buffer{}
	if err := json.Indent(formatted, output.Bytes(), "", "  "); err != nil {
		fmt.Println("Error indenting:", err)
		return ""
	}
	return string(formatted.Bytes())
}

//// PodStartupLatencyToPerfData transforms PodStartupLatency to PerfData.
func PodStartupLatencyToPerfData(latency *PodStartupLatency) *perftype.PerfData {
	perfData := &perftype.PerfData{Version: currentApiCallMetricsVersion}
	perfData.DataItems = append(perfData.DataItems, latencyToPerfData(latency.CreateToScheduleLatency, "create_to_schedule"))
	perfData.DataItems = append(perfData.DataItems, latencyToPerfData(latency.ScheduleToRunLatency, "schedule_to_run"))
	perfData.DataItems = append(perfData.DataItems, latencyToPerfData(latency.RunToWatchLatency, "run_to_watch"))
	perfData.DataItems = append(perfData.DataItems, latencyToPerfData(latency.ScheduleToWatchLatency, "schedule_to_watch"))
	perfData.DataItems = append(perfData.DataItems, latencyToPerfData(latency.E2ELatency, "pod_startup"))
	return perfData
}

func latencyToPerfData(l LatencyMetric, name string) perftype.DataItem {
	return perftype.DataItem{
		Data: map[string]float64{
			"Perc50":  float64(l.Perc50) / 1000000, //// us -> ms
			"Perc90":  float64(l.Perc90) / 1000000,
			"Perc99":  float64(l.Perc99) / 1000000,
			"Perc100": float64(l.Perc100) / 1000000,
		},
		Unit: "ms",
		Labels: map[string]string{
			"Metric": name,
		},
	}
}

func PrintLatencies(latencies []PodLatencyData, header string) {
	metrics := ExtractLatencyMetrics(latencies)
	fmt.Println("", header, latencies[(len(latencies)*9)/10:])
	fmt.Println("perc50:, perc90:, perc99:", metrics.Perc50, metrics.Perc90, metrics.Perc99)
}
