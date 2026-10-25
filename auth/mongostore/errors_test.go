package mongostore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/database"
)

func TestMapError(t *testing.T) {
	dupWriteErr := mongo.WriteException{
		WriteErrors: mongo.WriteErrors{
			{Code: 11000, Message: "E11000 duplicate key error collection: corego.corego_users index: ux_users_email"},
		},
	}
	dupCmdErr := mongo.CommandError{Name: "DuplicateKey", Code: 11000, Message: "E11000 duplicate key error"}
	dupUpdateErr := mongo.WriteException{WriteErrors: mongo.WriteErrors{{Code: 11001}}}
	otherWriteErr := mongo.WriteException{WriteErrors: mongo.WriteErrors{{Code: 121, Message: "Document failed validation"}}}
	plainErr := errors.New("connection refused")

	tests := []struct {
		name string
		in   error
		want error // nil means: mapError must return nil
	}{
		{name: "nil maps to nil", in: nil, want: nil},
		{name: "ErrNoDocuments maps to ErrNotFound", in: mongo.ErrNoDocuments, want: auth.ErrNotFound},
		{name: "wrapped ErrNoDocuments maps to ErrNotFound", in: fmt.Errorf("find user: %w", mongo.ErrNoDocuments), want: auth.ErrNotFound},
		{name: "database not found error maps to ErrNotFound", in: fmt.Errorf("%w: %w", database.ErrNotFound, mongo.ErrNoDocuments), want: auth.ErrNotFound},
		{name: "duplicate write exception maps to ErrDuplicate", in: dupWriteErr, want: auth.ErrDuplicate},
		{name: "wrapped duplicate write exception maps to ErrDuplicate", in: fmt.Errorf("insert: %w", dupWriteErr), want: auth.ErrDuplicate},
		{name: "duplicate command error maps to ErrDuplicate", in: dupCmdErr, want: auth.ErrDuplicate},
		{name: "wrapped duplicate command error maps to ErrDuplicate", in: fmt.Errorf("insert: %w", dupCmdErr), want: auth.ErrDuplicate},
		{name: "duplicate on update (11001) maps to ErrDuplicate", in: dupUpdateErr, want: auth.ErrDuplicate},
		{name: "context errors pass through", in: context.DeadlineExceeded, want: context.DeadlineExceeded},
		{name: "plain errors pass through", in: plainErr, want: plainErr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapError(tt.in)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("mapError(%v) = %v, want nil", tt.in, got)
				}
				return
			}
			if !errors.Is(got, tt.want) {
				t.Fatalf("mapError(%v) = %v, want %v", tt.in, got, tt.want)
			}
			if tt.want != auth.ErrNotFound && errors.Is(got, auth.ErrNotFound) {
				t.Fatalf("mapError(%v) unexpectedly matches ErrNotFound", tt.in)
			}
			if tt.want != auth.ErrDuplicate && errors.Is(got, auth.ErrDuplicate) {
				t.Fatalf("mapError(%v) unexpectedly matches ErrDuplicate", tt.in)
			}
		})
	}

	t.Run("other write errors pass through unchanged", func(t *testing.T) {
		got := mapError(otherWriteErr)
		if !reflect.DeepEqual(got, error(otherWriteErr)) {
			t.Fatalf("mapError(%v) = %#v, want the input unchanged", otherWriteErr, got)
		}
		if errors.Is(got, auth.ErrNotFound) || errors.Is(got, auth.ErrDuplicate) {
			t.Fatalf("mapError(%v) = %v, want no auth sentinel", otherWriteErr, got)
		}
	})
}

func TestIsIndexConflict(t *testing.T) {
	tests := []struct {
		name string
		in   error
		want bool
	}{
		{name: "nil", in: nil, want: false},
		{name: "IndexOptionsConflict (85)", in: mongo.CommandError{Code: 85, Name: "IndexOptionsConflict"}, want: true},
		{name: "IndexKeySpecsConflict (86)", in: mongo.CommandError{Code: 86, Name: "IndexKeySpecsConflict"}, want: true},
		{name: "wrapped IndexOptionsConflict", in: fmt.Errorf("createIndexes: %w", mongo.CommandError{Code: 85}), want: true},
		{name: "write exception with code 85", in: mongo.WriteException{WriteErrors: mongo.WriteErrors{{Code: 85}}}, want: true},
		{name: "write concern error with code 86", in: mongo.WriteException{WriteConcernError: &mongo.WriteConcernError{Code: 86}}, want: true},
		{name: "duplicate key (11000) while building", in: mongo.CommandError{Code: 11000}, want: false},
		{name: "unauthorized (13)", in: mongo.CommandError{Code: 13, Name: "Unauthorized"}, want: false},
		{name: "unrelated error", in: errors.New("boom"), want: false},
		{name: "context deadline", in: context.DeadlineExceeded, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isIndexConflict(tt.in); got != tt.want {
				t.Fatalf("isIndexConflict(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
