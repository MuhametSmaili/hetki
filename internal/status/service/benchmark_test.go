package service_test

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/MSmaili/hetki/internal/status"
	statusservice "github.com/MSmaili/hetki/internal/status/service"
	"github.com/stretchr/testify/require"
)

func benchmarkService(b *testing.B, count int) (*statusservice.Service, []statusservice.Pane) {
	b.Helper()

	panes := make([]statusservice.Pane, count)
	for i := range panes {
		panes[i] = fixturePane("")
		panes[i].Ref.ID = fmt.Sprintf("%%%d", i)
	}
	service, err := statusservice.New("server-birth")
	require.NoError(b, err)
	initial := inventory(1, panes...)
	initial.ValidUntil = time.Now().Add(24 * time.Hour)
	require.NoError(b, service.UpdateInventory(initial))
	for _, pane := range panes {
		target := targetFor(b, service, pane, statusservice.Protocol)
		require.NoError(b, service.Apply(target, 1, status.Value{State: status.Working}))
	}
	_, err = service.ReadStatus(context.Background())
	require.NoError(b, err)
	return service, panes
}

func BenchmarkCachedSnapshot(b *testing.B) {
	for _, count := range []int{0, 40, status.MaxPanes} {
		b.Run(fmt.Sprintf("panes=%d", count), func(b *testing.B) {
			service, _ := benchmarkService(b, count)
			b.ReportAllocs()

			for b.Loop() {
				if _, err := service.ReadStatus(context.Background()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkEvidenceAndSnapshot(b *testing.B) {
	for _, count := range []int{40, status.MaxPanes} {
		b.Run(fmt.Sprintf("panes=%d", count), func(b *testing.B) {
			service, panes := benchmarkService(b, count)
			target := targetFor(b, service, panes[0], statusservice.Protocol)
			sequence := uint64(1)
			states := [...]status.State{status.Blocked, status.Working}
			b.ReportAllocs()

			for b.Loop() {
				sequence++
				if err := service.Apply(target, sequence, status.Value{State: states[sequence%2]}); err != nil {
					b.Fatal(err)
				}
				if _, err := service.ReadStatus(context.Background()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkInventoryRefresh(b *testing.B) {
	for _, count := range []int{0, 40, status.MaxPanes} {
		b.Run(fmt.Sprintf("panes=%d", count), func(b *testing.B) {
			service, panes := benchmarkService(b, count)
			next := inventory(1, panes...)
			next.ValidUntil = time.Now().Add(24 * time.Hour)
			b.ReportAllocs()

			for b.Loop() {
				next.Revision++
				if err := service.UpdateInventory(next); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRepeatedEvidence(b *testing.B) {
	service, panes := benchmarkService(b, 40)
	target := targetFor(b, service, panes[0], statusservice.Protocol)
	sequence := uint64(1)
	b.ReportAllocs()

	for b.Loop() {
		sequence++
		if err := service.Apply(target, sequence, status.Value{State: status.Working}); err != nil {
			b.Fatal(err)
		}
		if _, err := service.ReadStatus(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRetainedCache(b *testing.B) {
	for _, count := range []int{40, status.MaxPanes} {
		b.Run(fmt.Sprintf("panes=%d", count), func(b *testing.B) {
			for b.Loop() {
				b.ReportMetric(retainedCacheBytes(b, count), "retained-B/cache")
			}
		})
	}
}

func retainedCacheBytes(b *testing.B, count int) float64 {
	b.Helper()

	const copies = 32
	var before, after runtime.MemStats
	var services [copies]*statusservice.Service
	runtime.GC()
	runtime.ReadMemStats(&before)

	for i := range services {
		services[i], _ = benchmarkService(b, count)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(services)

	return float64(int64(after.HeapAlloc)-int64(before.HeapAlloc)) / copies
}
