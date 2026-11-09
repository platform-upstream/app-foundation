package identity

import (
	"context"
	"testing"
)

type fixedResolver struct{ person Person }

func (r fixedResolver) ByEmail(context.Context, string) (Person, error) { return r.person, nil }
func (r fixedResolver) ByID(context.Context, string) (Person, error)    { return r.person, nil }

// A handler that changes the Spaces of its answer must not change what the next request gets:
// Spaces decide which data a person may read and write (InSpace).
func TestCacheAnswersDoNotShareSpaces(t *testing.T) {
	home := Space{Kind: SpaceFamily, ID: "home"}
	other := Space{Kind: SpaceFamily, ID: "other"}
	person := Person{ID: "alice", Role: "member", Spaces: []Space{{Kind: SpacePerson, ID: "alice"}, home}}
	cache := NewCache(fixedResolver{person}, CacheOptions{})

	for _, step := range []string{"fetched", "cached"} {
		answer, err := cache.ByID(context.Background(), "alice")
		if err != nil {
			t.Fatal(err)
		}
		if !answer.InSpace(home) || answer.InSpace(other) {
			t.Fatalf("%s answer has the spaces an earlier answer was changed to: %v", step, answer.Spaces)
		}
		answer.Spaces[1] = other
	}
	answer, err := cache.ByID(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !answer.InSpace(home) || answer.InSpace(other) {
		t.Fatalf("answer has the spaces an earlier answer was changed to: %v", answer.Spaces)
	}
}
