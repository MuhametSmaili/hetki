package osc7501

import (
	"strings"
	"testing"
)

func BenchmarkObserver(b *testing.B) {
	cases := []struct {
		name    string
		payload string
	}{
		{"state", "state=working"},
		{"metadata", "state=blocked:kind=permission:app=worker:msg=QXBwcm92ZT8="},
		{"overlong malformed field", "state=idle:msg=" + strings.Repeat("!", 2733)},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			var observer Observer
			payload := []byte(tc.payload)
			observer.Handle(payload)
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				observer.Handle(payload)
			}
		})
	}
}
