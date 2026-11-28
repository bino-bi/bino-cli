package daemon

import (
	"context"

	"bino.bi/bino/internal/report/dataquery"
)

// QueryDataResult is the result of QueryData.
type QueryDataResult struct {
	dataquery.QueryResult
	Error string `json:"error,omitempty"`
}

// ProfileDataResult is the result of ProfileData.
type ProfileDataResult struct {
	dataquery.Profile
	File  string `json:"file,omitempty"`
	Error string `json:"error,omitempty"`
}

// QueryData runs one SELECT over the project's DataSources and DataSets on a
// session of its own, never the shared one (see dataquery.Query).
func (s *State) QueryData(ctx context.Context, sql string, limit int) QueryDataResult {
	s.mu.RLock()
	docs, ambiguous := s.introspect, s.ambiguous
	s.mu.RUnlock()
	res, err := dataquery.Query(ctx, docs, ambiguous, sql, limit)
	out := QueryDataResult{QueryResult: res}
	if err != nil {
		out.Error = err.Error()
	}
	return out
}

// ProfileData profiles one DataSet or DataSource, resolved like Rows resolves
// a name, on a session of its own (see dataquery.ProfileData).
func (s *State) ProfileData(ctx context.Context, name string, sampleRows int) ProfileDataResult {
	out := ProfileDataResult{Profile: dataquery.Profile{Name: name, Columns: []dataquery.ColumnProfile{}, Warnings: []string{}}}
	doc, ambiguous, err := s.target(name)
	if err == nil {
		out.File = doc.File
		s.mu.RLock()
		docs := s.introspect
		s.mu.RUnlock()
		out.Profile, err = dataquery.ProfileData(ctx, docs, ambiguous, doc, sampleRows)
	}
	if err != nil {
		out.Error = err.Error()
	}
	return out
}
