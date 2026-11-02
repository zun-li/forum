package vote

import (
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"forum/internal/testutil"
)

func TestVotingAndRankings(t *testing.T) {
	ctx := t.Context()
	client := testutil.Redis(t)
	s := NewStore(client)
	const first, second, third uint64 = 999999999999999931, 999999999999999932, 999999999999999933
	created := time.Now().Add(-time.Hour)
	for i, p := range []struct{ id, community uint64 }{{first, 5}, {second, 5}, {third, 6}} {
		if err := s.IndexPost(ctx, p.id, p.community, created.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	// Concurrent duplicate requests must change the score exactly once.
	const workers = 16
	result := make(chan error, workers)
	for range workers {
		go func() { result <- s.Cast(ctx, 1, first, 1) }()
	}
	successes := 0
	for range workers {
		err := <-result
		switch {
		case err == nil:
			successes++
		case !errors.Is(err, ErrVoteRepeated):
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("vote applied %d times", successes)
	}

	check := func(score float64, votes int64) {
		t.Helper()
		for _, key := range []string{scoreIndex, communityIndex("5", "score")} {
			got, err := client.ZScore(ctx, key, strconv.FormatUint(first, 10)).Result()
			if err != nil || got != score {
				t.Fatalf("%s score = %f, err %v; want %f", key, got, err, score)
			}
		}
		counts, err := s.Upvotes(ctx, []uint64{first, third})
		if err != nil || counts[first] != votes || counts[third] != 0 {
			t.Fatalf("counts = %v, err %v; want %d upvotes", counts, err, votes)
		}
	}
	base := float64(created.Unix())
	check(base+pointsPerVote, 1)
	if err := s.IndexPost(ctx, first, 5, created); err != nil {
		t.Fatal(err)
	}
	check(base+pointsPerVote, 1)

	for _, test := range []struct {
		community  uint64
		order      string
		page, size int64
		want       []uint64
	}{
		{0, "time", 1, 3, []uint64{third, second, first}},
		{0, "score", 1, 3, []uint64{first, third, second}},
		{5, "score", 1, 1, []uint64{first}},
		{5, "score", 2, 1, []uint64{second}},
		{6, "time", 2, 1, []uint64{}},
	} {
		got, err := s.ListIDs(ctx, test.community, test.order, test.page, test.size)
		if err != nil || !slices.Equal(got, test.want) {
			t.Errorf("ListIDs(%+v) got %v, err %v", test, got, err)
		}
	}
	// Switching sides moves the score by 2*432; cancel restores it.
	if err := s.Cast(ctx, 1, first, -1); err != nil {
		t.Fatal(err)
	}
	check(base-pointsPerVote, 0)
	if err := s.Cast(ctx, 1, first, 0); err != nil {
		t.Fatal(err)
	}
	check(base, 0)
	if err := s.Cast(ctx, 1, first, 0); !errors.Is(err, ErrVoteRepeated) {
		t.Fatal("unexpected repeat result", err)
	}
	if err := s.Cast(ctx, 1, first, 8); !errors.Is(err, ErrInvalidVote) {
		t.Fatal("unexpected direction result", err)
	}
	if err := s.Cast(ctx, 1, 123, 1); !errors.Is(err, ErrPostNotFound) {
		t.Fatal("unexpected missing post result", err)
	}
	if err := s.IndexPost(ctx, 123, 5, created.Add(-voteWindow)); err != nil {
		t.Fatal(err)
	}
	if err := s.Cast(ctx, 1, 123, 1); !errors.Is(err, ErrVoteExpired) {
		t.Fatal("unexpected expired post result", err)
	}
}

func TestRemovePostSurvivesStaleReplay(t *testing.T) {
	ctx := t.Context()
	client := testutil.Redis(t)
	store := NewStore(client)
	created := time.Now()
	for _, id := range []uint64{10, 11} {
		if err := store.IndexPost(ctx, id, 3, created); err != nil {
			t.Fatal(err)
		}
		if err := store.Cast(ctx, 1, id, 1); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := store.RemovePost(ctx, 10, 3); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.IndexPost(ctx, 10, 3, created); err != nil {
		t.Fatal(err)
	}
	for _, community := range []uint64{0, 3} {
		for _, order := range []string{"time", "score"} {
			ids, err := store.ListIDs(ctx, community, order, 1, 10)
			if err != nil || !slices.Equal(ids, []uint64{11}) {
				t.Fatalf("community %d, order %s: %v (%v)", community, order, ids, err)
			}
		}
	}
	if err := store.Cast(ctx, 2, 10, 1); !errors.Is(err, ErrPostNotFound) {
		t.Fatalf("vote on deleted post: %v", err)
	}
	if n, err := client.Exists(ctx, votesKey("10")).Result(); err != nil || n != 0 {
		t.Fatalf("deleted post votes: %d (%v)", n, err)
	}
	if counts, err := store.Upvotes(ctx, []uint64{11}); err != nil || counts[11] != 1 {
		t.Fatalf("other post votes: %v (%v)", counts, err)
	}
}
