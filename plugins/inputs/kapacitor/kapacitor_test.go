package kapacitor_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/plugins/inputs/kapacitor"
	"github.com/influxdata/telegraf/testutil"
)

func TestKapacitor(t *testing.T) {
	kapacitorReturn, err := os.ReadFile("./testdata/kapacitor_return.json")
	require.NoError(t, err)

	fakeInfluxServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/endpoint" {
			if _, err := w.Write(kapacitorReturn); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
				return
			}
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fakeInfluxServer.Close()

	plugin := &kapacitor.Kapacitor{
		URLs: []string{fakeInfluxServer.URL + "/endpoint"},
	}

	var acc testutil.Accumulator
	require.NoError(t, plugin.Gather(&acc))

	require.Len(t, acc.Metrics, 63)

	fields := map[string]interface{}{
		"alloc_bytes":         int64(6950624),
		"buck_hash_sys_bytes": int64(1446737),
		"frees":               int64(129656),
		"gc_cpu_fraction":     float64(0.006757149597237818),
		"gc_sys_bytes":        int64(575488),
		"heap_alloc_bytes":    int64(6950624),
		"heap_idle_bytes":     int64(499712),
		"heap_in_use_bytes":   int64(9166848),
		"heap_objects":        int64(28070),
		"heap_released_bytes": int64(0),
		"heap_sys_bytes":      int64(9666560),
		"last_gc_ns":          int64(1478813691405406556),
		"lookups":             int64(40),
		"mallocs":             int64(157726),
		"mcache_in_use_bytes": int64(9600),
		"mcache_sys_bytes":    int64(16384),
		"mspan_in_use_bytes":  int64(105600),
		"mspan_sys_bytes":     int64(114688),
		"next_gc_ns":          int64(10996691),
		"num_gc":              int64(4),
		"other_sys_bytes":     int64(1985959),
		"pause_total_ns":      int64(767327),
		"stack_in_use_bytes":  int64(819200),
		"stack_sys_bytes":     int64(819200),
		"sys_bytes":           int64(14625016),
		"total_alloc_bytes":   int64(13475176),
	}

	tags := map[string]string{
		"kap_version": "1.1.0~rc2",
		"url":         fakeInfluxServer.URL + "/endpoint",
	}
	acc.AssertContainsTaggedFields(t, "kapacitor_memstats", fields, tags)

	acc.AssertContainsTaggedFields(t, "kapacitor",
		map[string]interface{}{
			"num_enabled_tasks": 5,
			"num_subscriptions": 6,
			"num_tasks":         5,
		}, tags)
}

func TestKapacitorAddsURLTagToNestedMetrics(t *testing.T) {
	serverA := newKapacitorTestServer(t, kapacitorNestedResponse("task-a", "main-a", 1, 2, 3, 4))
	defer serverA.Close()
	serverB := newKapacitorTestServer(t, kapacitorNestedResponse("task-b", "main-b", 5, 6, 7, 8))
	defer serverB.Close()

	plugin := &kapacitor.Kapacitor{
		URLs: []string{serverA.URL, serverB.URL},
	}

	var acc testutil.Accumulator
	require.NoError(t, plugin.Gather(&acc))
	require.Empty(t, acc.Errors)

	assertNestedURLTags(t, &acc, serverA.URL, "task-a", "main-a", 1, 2, 3, 4)
	assertNestedURLTags(t, &acc, serverB.URL, "task-b", "main-b", 5, 6, 7, 8)
}

func TestMissingStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(`{}`)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))
	defer server.Close()

	plugin := &kapacitor.Kapacitor{
		URLs: []string{server.URL},
	}

	var acc testutil.Accumulator
	require.NoError(t, plugin.Gather(&acc))

	require.False(t, acc.HasField("kapacitor_memstats", "alloc_bytes"))
	require.True(t, acc.HasField("kapacitor", "num_tasks"))
}

func TestErrorHandling(t *testing.T) {
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/endpoint" {
			if _, err := w.Write([]byte("not json")); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				t.Error(err)
				return
			}
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer badServer.Close()

	plugin := &kapacitor.Kapacitor{
		URLs: []string{badServer.URL + "/endpoint"},
	}

	var acc testutil.Accumulator
	require.NoError(t, plugin.Gather(&acc))
	acc.WaitError(1)
	require.Equal(t, uint64(0), acc.NMetrics())
}

func newKapacitorTestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(body)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))
}

func kapacitorNestedResponse(task, taskMaster string, collected, emitted, pointsReceived, loadErrors int) string {
	return fmt.Sprintf(`{
	"version": "1.7.6",
	"num_enabled_tasks": 1,
	"num_subscriptions": 2,
	"num_tasks": 3,
	"kapacitor": {
		"edge": {
			"name": "edges",
			"tags": {
				"task": %q,
				"parent": "write_points",
				"child": "stream",
				"type": "stream",
				"host": "localhost",
				"cluster_id": "cluster-id",
				"server_id": "server-id"
			},
			"values": {
				"collected": %d,
				"emitted": %d
			}
		},
		"ingress": {
			"name": "ingress",
			"tags": {
				"task_master": %q,
				"database": "_internal",
				"retention_policy": "monitor",
				"measurement": "runtime"
			},
			"values": {
				"points_received": %d
			}
		},
		"node": {
			"name": "nodes",
			"tags": {
				"task": %q,
				"node": "stream0",
				"type": "stream",
				"kind": "stream"
			},
			"values": {
				"avg_exec_time_ns": "2ms"
			}
		},
		"load": {
			"name": "load",
			"values": {
				"errors": %d
			}
		}
	}
}`, task, collected, emitted, taskMaster, pointsReceived, task, loadErrors)
}

func assertNestedURLTags(
	t *testing.T,
	acc *testutil.Accumulator,
	url, task, taskMaster string,
	collected, emitted, pointsReceived, loadErrors int,
) {
	t.Helper()

	acc.AssertContainsTaggedFields(t, "kapacitor_edges",
		map[string]interface{}{
			"collected": float64(collected),
			"emitted":   float64(emitted),
		},
		map[string]string{
			"task":   task,
			"parent": "write_points",
			"child":  "stream",
			"type":   "stream",
			"url":    url,
		})

	acc.AssertContainsTaggedFields(t, "kapacitor_ingress",
		map[string]interface{}{
			"points_received": float64(pointsReceived),
		},
		map[string]string{
			"task_master":      taskMaster,
			"database":         "_internal",
			"retention_policy": "monitor",
			"measurement":      "runtime",
			"url":              url,
		})

	acc.AssertContainsTaggedFields(t, "kapacitor_nodes",
		map[string]interface{}{
			"avg_exec_time_ns": int64(2 * 1000 * 1000),
		},
		map[string]string{
			"task": task,
			"node": "stream0",
			"type": "stream",
			"kind": "stream",
			"url":  url,
		})

	acc.AssertContainsTaggedFields(t, "kapacitor_load",
		map[string]interface{}{
			"errors": float64(loadErrors),
		},
		map[string]string{
			"url": url,
		})
}

func TestErrorHandling404(t *testing.T) {
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer badServer.Close()

	plugin := &kapacitor.Kapacitor{
		URLs: []string{badServer.URL},
	}

	var acc testutil.Accumulator
	require.NoError(t, plugin.Gather(&acc))
	acc.WaitError(1)
	require.Equal(t, uint64(0), acc.NMetrics())
}
