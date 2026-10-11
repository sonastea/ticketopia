package eventactivity

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type previewStub struct {
	ids []string
	err error
}

func (r *previewStub) Previews(_ context.Context, ids []string) (map[string]Preview, error) {
	r.ids = ids
	if r.err != nil {
		return nil, r.err
	}
	return map[string]Preview{ids[0]: {Questions: 2, Question: &Contribution{Body: "A public question"}}}, nil
}

func TestPreviewBatchValidationAndFailure(t *testing.T) {
	repo := &previewStub{}
	s := New(repo)
	for _, ids := range [][]string{{"invalid"}, make([]string, 101)} {
		if _, err := s.Previews(t.Context(), ids); err == nil || repo.ids != nil {
			t.Fatal("invalid batch reached repository")
		}
	}
	if result, err := s.Previews(t.Context(), nil); err != nil || len(result) != 0 || repo.ids != nil {
		t.Fatal("empty batch queried repository", result, err)
	}
	result, err := s.Previews(t.Context(), []string{"ticketmaster:one", "ticketmaster:one", "ticketmaster:two"})
	if err != nil || !reflect.DeepEqual(repo.ids, []string{"ticketmaster:one", "ticketmaster:two"}) || result["ticketmaster:one"].Questions != 2 {
		t.Fatal("batch lost event identity or duplicate elimination", result, err)
	}
	repo.err = ErrUnavailable
	if result, err := s.Previews(t.Context(), []string{"ticketmaster:one"}); !errors.Is(err, ErrUnavailable) || result != nil {
		t.Fatal("failure fabricated empty activity", result, err)
	}
}
