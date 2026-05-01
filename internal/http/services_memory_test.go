package http

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"ux-anomalies-api/internal/anomalies"
)

func TestAnomalyValidation(t *testing.T) {
	s := NewInMemoryServices()
	uid := uuid.New()
	_, err := s.Anomalies.Create(context.Background(), uid, anomalies.CreateInput{
		Category:    anomalies.CategoryWeb,
		Description: "",
		Images:      []anomalies.Image{{Bucket: "b", ObjectName: "o", MimeType: "image/png", ByteSize: 10, Position: 1}},
	})
	if err == nil {
		t.Fatal("expected description validation error")
	}
	_, err = s.Anomalies.Create(context.Background(), uid, anomalies.CreateInput{
		Category:    anomalies.CategoryWeb,
		Description: "ok",
		Images:      []anomalies.Image{},
	})
	if err == nil {
		t.Fatal("expected image count validation error")
	}
}

func TestLikeIdempotency(t *testing.T) {
	s := NewInMemoryServices()
	uid := uuid.New()
	a, err := s.Anomalies.Create(context.Background(), uid, anomalies.CreateInput{
		Category:    anomalies.CategoryWeb,
		Description: "ok",
		Images:      []anomalies.Image{{Bucket: "b", ObjectName: "o", MimeType: "image/png", ByteSize: 10, Position: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Anomalies.Like(context.Background(), uid, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Anomalies.Like(context.Background(), uid, a.ID); err != nil {
		t.Fatal(err)
	}
	updated, err := s.Anomalies.GetByID(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LikeCount != 1 {
		t.Fatalf("expected like_count=1 got=%d", updated.LikeCount)
	}
}

func TestCommentValidation(t *testing.T) {
	s := NewInMemoryServices()
	uid := uuid.New()
	a, err := s.Anomalies.Create(context.Background(), uid, anomalies.CreateInput{
		Category:    anomalies.CategoryWeb,
		Description: "ok",
		Images:      []anomalies.Image{{Bucket: "b", ObjectName: "o", MimeType: "image/png", ByteSize: 10, Position: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Comments.Create(context.Background(), a.ID, uid, " ")
	if err == nil {
		t.Fatal("expected comment text validation error")
	}
}
