package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsUniqueViolation reports a unique_violation (23505) on the named constraint
// or index, so repositories can translate it into a domain error.
func IsUniqueViolation(err error, constraint string) bool {
	return isConstraintError(err, "23505", constraint)
}

// IsExclusionViolation reports an exclusion_violation (23P01), e.g. overlapping leases.
func IsExclusionViolation(err error, constraint string) bool {
	return isConstraintError(err, "23P01", constraint)
}

func isConstraintError(err error, code, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code && pgErr.ConstraintName == constraint
}
