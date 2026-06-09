package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

func TestGetUsageUnitsWithFilters_UsesDev2TotalCostUnits(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer db.Close()

	repo := &usageLogRepository{sql: db}
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta("billing_mode = 'dev2_units'")).
		WithArgs(int64(20), start, end).
		WillReturnRows(sqlmock.NewRows([]string{"units"}).AddRow(19.32))

	got, err := repo.GetUsageUnitsWithFilters(context.Background(), usagestats.UsageLogFilters{
		UserID:    20,
		StartTime: &start,
		EndTime:   &end,
	})

	require.NoError(t, err)
	require.Equal(t, 19.32, got)
	require.NoError(t, mock.ExpectationsWereMet())
}
