package runtimeshapes

import (
	"reflect"
	"testing"
)

type scenario struct {
	name  string
	steps []call
	check func(*testing.T, state, []reply)
}

func scenarios() []scenario {
	return []scenario{
		{
			name: "registration distinguishes recursion from concurrency",
			steps: []call{
				{name: "register", input: registerInput{}},
				{name: "register", input: registerInput{}},
				{name: "register", input: registerInput{lineage: 1}},
			},
			check: func(t *testing.T, got state, replies []reply) {
				t.Helper()
				if len(got.campaigns) != 2 || !replies[0].accepted || !replies[1].accepted || !replies[2].recursive {
					t.Fatalf("registration: state=%#v replies=%#v", got, replies)
				}
			},
		},
		{
			name: "exclusive request is a FIFO barrier",
			steps: []call{
				{name: "register", input: registerInput{}},
				{name: "register", input: registerInput{}},
				{name: "register", input: registerInput{}},
				{name: "request", input: requestInput{campaign: 1, attempt: "a1", class: shared}},
				{name: "request", input: requestInput{campaign: 1, attempt: "a2", class: shared}},
				{name: "request", input: requestInput{campaign: 2, attempt: "b1", class: shared}},
				{name: "request", input: requestInput{campaign: 3, attempt: "x", class: exclusive}},
				{name: "request", input: requestInput{campaign: 2, attempt: "b2", class: shared}},
				{name: "cancel", input: cancelInput{grant: grant{campaign: 1, attempt: "a1"}}},
				{name: "cancel", input: cancelInput{grant: grant{campaign: 1, attempt: "a2"}}},
				{name: "cancel", input: cancelInput{grant: grant{campaign: 2, attempt: "b1"}}},
				{name: "cancel", input: cancelInput{grant: grant{campaign: 3, attempt: "x"}}},
			},
			check: func(t *testing.T, _ state, replies []reply) {
				t.Helper()
				got := []grant{}
				for _, item := range replies {
					got = append(got, item.deliveries...)
				}
				want := []grant{{1, "a1"}, {1, "a2"}, {2, "b1"}, {3, "x"}, {2, "b2"}}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("grant order=%#v, want %#v", got, want)
				}
			},
		},
		{
			name: "only committed obligations overlap",
			steps: []call{
				{name: "register", input: registerInput{}},
				{name: "register", input: registerInput{}},
				{name: "request", input: requestInput{campaign: 1, attempt: "a", class: shared}},
				{name: "request", input: requestInput{campaign: 2, attempt: "b", class: shared}},
				{name: "start", input: startInput{grant: grant{campaign: 1, attempt: "a"}}},
				{name: "start", input: startInput{grant: grant{campaign: 2, attempt: "b"}}},
				{name: "observe", input: observationInput{generation: 1, kind: launchOwned}},
				{name: "observe", input: observationInput{generation: 2, kind: launchOwned}},
			},
			check: func(t *testing.T, got state, replies []reply) {
				t.Helper()
				if replies[4].generation != 1 || replies[5].generation != 2 || !got.entries[0].overlapped || !got.entries[1].overlapped {
					t.Fatalf("overlap: state=%#v replies=%#v", got, replies)
				}
			},
		},
		{
			name: "overlap deadline closes gate and installs unbound barrier",
			steps: []call{
				{name: "register", input: registerInput{}},
				{name: "register", input: registerInput{}},
				{name: "request", input: requestInput{campaign: 1, attempt: "a", class: shared}},
				{name: "request", input: requestInput{campaign: 2, attempt: "b", class: shared}},
				{name: "start", input: startInput{grant: grant{campaign: 1, attempt: "a"}}},
				{name: "start", input: startInput{grant: grant{campaign: 2, attempt: "b"}}},
				{name: "observe", input: observationInput{generation: 1, kind: launchOwned}},
				{name: "observe", input: observationInput{generation: 2, kind: launchOwned}},
				{name: "observe", input: observationInput{generation: 1, kind: deadline}},
				{name: "observe", input: observationInput{generation: 2, kind: settled}},
				{name: "bind", input: bindInput{campaign: 1, attempt: "confirm-a"}},
			},
			check: func(t *testing.T, got state, replies []reply) {
				t.Helper()
				if got.campaigns[0].gateOpen || len(got.entries) != 1 || got.entries[0].stage != granted || len(replies[10].deliveries) != 1 {
					t.Fatalf("barrier: state=%#v reply=%#v", got, replies[10])
				}
			},
		},
		{
			name: "hard launch pressure is one way and does not revoke active work",
			steps: []call{
				{name: "register", input: registerInput{}},
				{name: "register", input: registerInput{}},
				{name: "request", input: requestInput{campaign: 1, attempt: "a", class: shared}},
				{name: "request", input: requestInput{campaign: 2, attempt: "b", class: shared}},
				{name: "start", input: startInput{grant: grant{campaign: 1, attempt: "a"}}},
				{name: "start", input: startInput{grant: grant{campaign: 2, attempt: "b"}}},
				{name: "observe", input: observationInput{generation: 1, kind: launchResourceExhausted}},
				{name: "request", input: requestInput{campaign: 1, attempt: "c", class: shared}},
				{name: "observe", input: observationInput{generation: 2, kind: launchOwned}},
				{name: "observe", input: observationInput{generation: 2, kind: settled}},
			},
			check: func(t *testing.T, got state, replies []reply) {
				t.Helper()
				if !got.single || len(replies[6].deliveries) != 0 || len(replies[9].deliveries) != 1 {
					t.Fatalf("pressure: state=%#v replies=%#v", got, replies)
				}
			},
		},
		{
			name: "drain unconfirmed closes without releasing custody",
			steps: []call{
				{name: "register", input: registerInput{}},
				{name: "request", input: requestInput{campaign: 1, attempt: "a", class: shared}},
				{name: "start", input: startInput{grant: grant{campaign: 1, attempt: "a"}}},
				{name: "observe", input: observationInput{generation: 1, kind: launchOwned}},
				{name: "observe", input: observationInput{generation: 1, kind: drainUnconfirmed}},
				{name: "request", input: requestInput{campaign: 1, attempt: "b", class: shared}},
			},
			check: func(t *testing.T, got state, replies []reply) {
				t.Helper()
				if !got.closed || len(got.entries) != 1 || got.entries[0].stage != owned || !replies[5].closed {
					t.Fatalf("fatal custody: state=%#v replies=%#v", got, replies)
				}
			},
		},
		{
			name: "terminal commit and emergency settlement are acknowledged",
			steps: []call{
				{name: "register", input: registerInput{}},
				{name: "request", input: requestInput{campaign: 1, attempt: "a", class: shared}},
				{name: "start", input: startInput{grant: grant{campaign: 1, attempt: "a"}}},
				{name: "observe", input: observationInput{generation: 1, kind: launchOwned}},
				{name: "close", input: closeInput{}},
				{name: "terminal", input: terminalInput{campaign: 1}},
				{name: "settle", input: settleInput{generation: 1}},
			},
			check: func(t *testing.T, got state, replies []reply) {
				t.Helper()
				if replies[5].accepted || !replies[5].closed || !replies[6].accepted || len(got.entries) != 0 {
					t.Fatalf("settlement: state=%#v replies=%#v", got, replies)
				}
			},
		},
	}
}

func testShape(t *testing.T, implementation shape) {
	t.Helper()
	for _, test := range scenarios() {
		t.Run(test.name, func(t *testing.T) {
			got := state{capacity: 2}
			replies := make([]reply, 0, len(test.steps))
			for _, step := range test.steps {
				var item reply
				got, item = implementation.apply(got, step)
				replies = append(replies, item)
			}
			test.check(t, got, replies)
		})
	}
}

func TestTypedMethodsShape(t *testing.T) { testShape(t, methodsShape{}) }
