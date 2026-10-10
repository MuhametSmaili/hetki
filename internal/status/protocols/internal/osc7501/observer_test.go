package osc7501

import (
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReportsValidateBeforeMutatingRecords(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		result  Result
	}{
		{"query", "?", Query},
		{"missing state", "app=worker", Ignored},
		{"unknown state", "state=busy", Ignored},
		{"last unknown state", "state=idle:state=busy", Ignored},
		{"empty id", "state=idle:id=", Ignored},
		{"empty segment", "state=idle:id=build//test", Ignored},
		{"malformed id cannot address root", "state=idle:id=bad!", Ignored},
		{"oversized sequence", "state=idle:" + strings.Repeat("x", maxPayloadBytes), Rejected},
		{"oversized malformed key", "state=idle:" + strings.Repeat("!", 17), Rejected},
		{"overlong app before duplicate", "state=idle:app=" + strings.Repeat("a", 33) + ":app=ok", Rejected},
		{"oversized malformed msg", "state=idle:msg=" + strings.Repeat("!", 2733), Rejected},
		{"oversized malformed title", "state=idle:title=" + strings.Repeat("!", 257), Rejected},
		{"overlong id segment before duplicate", "state=idle:id=" + strings.Repeat("a", 33) + ":id=ok", Rejected},
		{"overlong id", "state=idle:id=" + strings.Repeat("a/", 64) + "a", Rejected},
		{"too many id levels", "state=idle:id=" + strings.Repeat("a/", 8) + "a", Rejected},
		{"invalid base64 before duplicate", "state=idle:msg=a:msg=b2s=", Rejected},
		{"invalid padding", "state=idle:title=b2s===", Rejected},
		{"invalid UTF8", "state=idle:msg=/w==", Rejected},
		{"invalid id cannot hide bad text", "state=idle:id=:msg=/w==", Rejected},
		{"unknown state cannot hide bad text", "state=new:msg=/w==", Rejected},
		{"oversized decoded message", "state=idle:msg=" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 2049))), Rejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var observer Observer
			require.Equal(t, Applied, observer.Handle([]byte("state=working")))
			require.Equal(t, Applied, observer.Handle([]byte("state=blocked:id=child")))
			before := slices.Clone(observer.records)

			result := observer.Handle([]byte(tc.payload))

			require.Equal(t, tc.result, result)
			require.Equal(t, before, observer.records)
		})
	}
}

func TestReportsAcceptValidStatesAndSkipMalformedPairs(t *testing.T) {
	maxID := strings.Repeat("a", 32) + "/" + strings.Repeat("b", 32) + "/" +
		strings.Repeat("c", 32) + "/" + strings.Repeat("d", 29)
	cases := []struct {
		name    string
		payload string
		id      string
		state   State
	}{
		{"idle", "state=idle", "", Idle},
		{"working", "state=working", "", Working},
		{"blocked", "state=blocked", "", Blocked},
		{"done", "state=done", "", Done},
		{"error", "state=error", "", Error},
		{"whitespace and unknown metadata", " state = idle :future=x:kind=unrecognized:progress=no", "", Idle},
		{"malformed pair skipped", "state=idle:noequals:=x:STATE=bad:app=!:msg=!", "", Idle},
		{"last valid repeated state", "state=working:state=idle:state=!", "", Idle},
		{"last repeated id", "state=idle:id=first:id=last", "last", Idle},
		{"last id repairs grammar", "state=idle:id=bad//path:id=ok", "ok", Idle},
		{"padded text", "state=idle:msg=aMOpbGxv:title=b2s=", "", Idle},
		{"unpadded text", "state=idle:msg=b2s:title=b2s", "", Idle},
		{"maximum msg", "state=idle:msg=" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 2048))), "", Idle},
		{"maximum title", "state=idle:title=" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 192))), "", Idle},
		{"maximum app and key", "state=idle:app=" + strings.Repeat("a", 32) + ":abcdefghijklmnop=x", "", Idle},
		{"maximum id length", "state=idle:id=" + maxID, maxID, Idle},
		{"maximum id levels", "state=idle:id=a/b/c/d/e/f/g/h", "a/b/c/d/e/f/g/h", Idle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var observer Observer

			result := observer.Handle([]byte(tc.payload))
			state, found := observer.Lookup(tc.id)

			require.Equal(t, Applied, result)
			require.True(t, found)
			require.Equal(t, tc.state, state)
			require.True(t, observer.Observed())
		})
	}
}

func TestEveryDecodedControlIsRejected(t *testing.T) {
	for c := rune(0); c <= 0x9f; c++ {
		if c >= 0x20 && c < 0x7f {
			continue
		}
		t.Run(fmt.Sprintf("U+%04X", c), func(t *testing.T) {
			var observer Observer
			text := base64.StdEncoding.EncodeToString([]byte(string(c)))

			result := observer.Handle([]byte("state=idle:msg=" + text))

			require.Equal(t, Rejected, result)
			require.False(t, observer.Observed())
		})
	}
}

func TestSubtreeClearPreservesSiblingsAndRoot(t *testing.T) {
	var observer Observer
	for _, report := range []string{"state=idle", "state=working:id=build", "state=blocked:id=build/test", "state=done:id=builder"} {
		require.Equal(t, Applied, observer.Handle([]byte(report)))
	}

	result := observer.Handle([]byte("state=clear:id=build"))

	require.Equal(t, Applied, result)
	require.Equal(t, []record{{id: "", state: Idle}, {id: "builder", state: Done}}, observer.records)
	require.Equal(t, Applied, observer.Handle([]byte("state=clear")))
	require.Empty(t, observer.records)
	require.True(t, observer.Observed(), "clear is withdrawal, not absence of reports")
}

func TestEvictionUsesLastUpdateNotCreationOrLookup(t *testing.T) {
	var observer Observer
	require.Equal(t, Applied, observer.Handle([]byte("state=idle")))
	for i := 1; i < MaxRecords; i++ {
		require.Equal(t, Applied, observer.Handle(fmt.Appendf(nil, "state=working:id=child%d", i)))
	}
	require.Equal(t, Applied, observer.Handle([]byte("state=idle")))
	_, found := observer.Lookup("child1")
	require.True(t, found)

	require.Equal(t, Applied, observer.Handle([]byte("state=done:id=new")))
	_, evicted := observer.Lookup("child1")
	root, rootFound := observer.Lookup("")

	require.False(t, evicted)
	require.True(t, rootFound)
	require.Equal(t, Idle, root)
	require.Len(t, observer.records, MaxRecords)
	require.LessOrEqual(t, cap(observer.records), MaxRecords)
}

func TestActivityEndRetainsExplicitOutcomesUntilReset(t *testing.T) {
	var observer Observer
	for _, state := range []State{Idle, Working, Blocked, Done, Error} {
		require.Equal(t, Applied, observer.Handle([]byte("state="+state+":id="+state)))
	}

	changed := observer.EndActivity()

	require.True(t, changed)
	require.Equal(t, []record{{id: "done", state: Done}, {id: "error", state: Error}}, observer.records)
	require.False(t, observer.EndActivity())
	observer.Reset()
	require.Empty(t, observer.records)
	require.False(t, observer.Observed())
}
