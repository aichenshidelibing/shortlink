package service

import (
	"testing"
	"time"
)

func TestNextLocalNoonAfter(t *testing.T) {
	loc := time.FixedZone("test", 8*3600)
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "before noon returns today noon",
			now:  time.Date(2026, 7, 21, 11, 59, 0, 0, loc),
			want: time.Date(2026, 7, 21, 12, 0, 0, 0, loc),
		},
		{
			name: "at noon returns tomorrow noon",
			now:  time.Date(2026, 7, 21, 12, 0, 0, 0, loc),
			want: time.Date(2026, 7, 22, 12, 0, 0, 0, loc),
		},
		{
			name: "after noon returns tomorrow noon",
			now:  time.Date(2026, 7, 21, 12, 1, 0, 0, loc),
			want: time.Date(2026, 7, 22, 12, 0, 0, 0, loc),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NextLocalNoonAfter(tc.now); !got.Equal(tc.want) {
				t.Fatalf("NextLocalNoonAfter()=%v want %v", got, tc.want)
			}
		})
	}
}

func TestSuffixRotationDueAfterNoonWhenLastChangeBeforeNoon(t *testing.T) {
	loc := time.FixedZone("test", 8*3600)
	now := time.Date(2026, 7, 21, 12, 1, 0, 0, loc)
	changedAt := time.Date(2026, 7, 21, 11, 59, 0, 0, loc)
	if !suffixRotationDue(changedAt, now) {
		t.Fatal("expected suffix rotation to be due after noon when last change was before noon")
	}
}

func TestSuffixRotationDueSkipsWhenAlreadyChangedAfterNoon(t *testing.T) {
	loc := time.FixedZone("test", 8*3600)
	now := time.Date(2026, 7, 21, 13, 0, 0, 0, loc)
	changedAt := time.Date(2026, 7, 21, 12, 1, 0, 0, loc)
	if suffixRotationDue(changedAt, now) {
		t.Fatal("expected suffix rotation to skip after it already changed after noon")
	}
}

func TestSuffixRotationDueCatchesUpAfterDowntime(t *testing.T) {
	loc := time.FixedZone("test", 8*3600)
	now := time.Date(2026, 7, 21, 18, 0, 0, 0, loc)
	changedAt := time.Date(2026, 7, 18, 12, 1, 0, 0, loc)
	if !suffixRotationDue(changedAt, now) {
		t.Fatal("expected suffix rotation to catch up after missed noons")
	}
}

func TestSuffixRotationDueBeforeNoonUsesPreviousNoon(t *testing.T) {
	loc := time.FixedZone("test", 8*3600)
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, loc)
	changedAfterPreviousNoon := time.Date(2026, 7, 20, 12, 1, 0, 0, loc)
	if suffixRotationDue(changedAfterPreviousNoon, now) {
		t.Fatal("expected rotation to skip before noon when previous noon was already covered")
	}
	changedBeforePreviousNoon := time.Date(2026, 7, 20, 11, 59, 0, 0, loc)
	if !suffixRotationDue(changedBeforePreviousNoon, now) {
		t.Fatal("expected rotation to be due before noon when previous noon was missed")
	}
}
