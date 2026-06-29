/*******************************************************************************
 * Copyright (c) 2026 Genome Research Ltd.
 *
 * Author: Sendu Bala <sb10@sanger.ac.uk>
 *
 * Permission is hereby granted, free of charge, to any person obtaining
 * a copy of this software and associated documentation files (the
 * "Software"), to deal in the Software without restriction, including
 * without limitation the rights to use, copy, modify, merge, publish,
 * distribute, sublicense, and/or sell copies of the Software, and to
 * permit persons to whom the Software is furnished to do so, subject to
 * the following conditions:
 *
 * The above copyright notice and this permission notice shall be included
 * in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
 * EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
 * MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
 * IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
 * CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
 * TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
 * SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 ******************************************************************************/

// People-to-studies queries (D4): a named PI/sponsor or a study_users role
// member to their studies. The faculty-sponsor surface matches the free-text
// study.faculty_sponsor; the (later) user surface matches study_users role
// membership. Both share the people-endpoint cascade: a never-synced cache
// returns an empty list joined with neverSyncedReadErr(), while a synced cache
// with no match returns an empty list and no error (there is no parent
// identifier to "not find").
package mlwh

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// facultySponsorField is the single study_mirror column the faculty-sponsor
// endpoint matches against (the named PI/sponsor free-text). It is wrapped in
// likeContainsClause so the case-insensitive substring match is the same
// dialect-portable `col LIKE ? ESCAPE '!'` form the study/sample searches use
// (NOT a literal '||'/CONCAT, which would mean logical-OR on MySQL).
var facultySponsorField = []string{"faculty_sponsor"}

// facultySponsorWhereClause is the WHERE body shared by StudiesForFacultySponsor
// and its count sibling: SQSCP rows whose faculty_sponsor contains the term
// (case-insensitive substring via likeContainsClause).
var facultySponsorWhereClause = `id_lims = 'SQSCP' AND ` + likeContainsClause(facultySponsorField)

// facultySponsorPageSQL selects the full study rows whose faculty_sponsor
// contains the term, ordered by id_study_lims for stable pagination and bounded
// by LIMIT/OFFSET. It selects the same studyMirrorSelectColumns the study
// queries scan (via scanStudyRow), so each row is a full Study.
var facultySponsorPageSQL = `SELECT ` + studyMirrorSelectColumns + ` FROM study_mirror WHERE ` + facultySponsorWhereClause +
	` ORDER BY id_study_lims LIMIT ? OFFSET ?`

// facultySponsorCountSQL counts the studies StudiesForFacultySponsor would
// return for the term: the identical id_lims filter and faculty_sponsor LIKE,
// with no LIMIT, so CountStudiesForFacultySponsor(name) equals
// len(StudiesForFacultySponsor(name, all)) for any name.
var facultySponsorCountSQL = `SELECT COUNT(*) FROM study_mirror WHERE ` + facultySponsorWhereClause

// studyUsersPersonFields are the three study_users_mirror columns the user
// endpoint matches the person term against (qualified so name is unambiguous in
// the study_mirror join). The term is a case-insensitive substring of any one of
// them, OR'd together via likeContainsClause (the same dialect-portable
// `col LIKE ? ESCAPE '!'` form, NOT a literal '||'), so a caller given only an
// email or login resolves the same studies as one given the name.
var studyUsersPersonFields = []string{"study_users_mirror.name", "study_users_mirror.login", "study_users_mirror.email"}

// studyUsersDefaultRoles is the DEFAULT role set the user endpoint filters to when
// role is "" (the empty override): the roles that denote a working association
// with a study. follower/slf_manager/lab_manager/administrator are excluded unless
// a non-empty role= widens (overrides) the set.
var studyUsersDefaultRoles = []string{"owner", "manager", "data_access_contact"}

// studyUsersJoin is the FROM/JOIN body shared by StudiesForUser and its count
// sibling: study_users_mirror joined to its study by id_study_tmp, scoped to
// SQSCP studies, with the person matched as a case-insensitive substring of
// name/login/email. The role predicate (an IN-set of the resolved roles) and the
// projection/ordering are appended by the callers.
var studyUsersJoin = `study_users_mirror INNER JOIN study_mirror ON study_mirror.id_study_tmp = study_users_mirror.id_study_tmp` +
	` WHERE study_mirror.id_lims = 'SQSCP' AND ` + likeContainsClause(studyUsersPersonFields)

// studyUsersStudyColumns is the qualified study_mirror column list the user
// endpoint selects (so the join is unambiguous), the same columns scanStudyRow
// scans, so each row is a full Study.
var studyUsersStudyColumns = qualifySelectColumns("study_mirror", studyMirrorSelectColumns)

// resolvePersonSponsorFields is the single study_mirror column the resolve-person
// faculty_sponsor source matches against, wrapped in likeContainsClause so the
// case-insensitive substring is the same dialect-portable `col LIKE ? ESCAPE '!'`
// form (NOT a literal '||'/CONCAT). It is the same field facultySponsorField names;
// kept separate so the two surfaces stay independently legible.
var resolvePersonSponsorFields = []string{"faculty_sponsor"}

// resolvePersonUserFields are the three study_users_mirror columns the
// resolve-person study_users source matches the term against (qualified so name is
// unambiguous in the study_mirror join). The term is a case-insensitive substring
// of any one of them, OR'd via likeContainsClause (NOT a literal '||').
var resolvePersonUserFields = []string{"study_users_mirror.name", "study_users_mirror.login", "study_users_mirror.email"}

// resolvePersonSponsorBranch is the faculty_sponsor candidate sub-select: one row
// per DISTINCT faculty_sponsor value containing the term (scoped to id_lims =
// 'SQSCP'), projecting the fixed Source plus empty login/email/role, grouped by
// faculty_sponsor so StudyCount is COUNT(DISTINCT id_study_lims) for THAT sponsor
// (the enumeration study_mirror_faculty_sponsor_idx backs). The columns line up
// positionally with resolvePersonUserBranch so the two UNION ALL together.
var resolvePersonSponsorBranch = `SELECT 'faculty_sponsor' AS source, faculty_sponsor AS name, ` +
	`'' AS login, '' AS email, '' AS role, COUNT(DISTINCT id_study_lims) AS study_count ` +
	`FROM study_mirror WHERE id_lims = 'SQSCP' AND ` + likeContainsClause(resolvePersonSponsorFields) +
	` GROUP BY faculty_sponsor`

// resolvePersonUserBranch is the study_users candidate sub-select: one row per
// DISTINCT (name, login, email, role) tuple (the candidate identity) where the term
// is a case-insensitive substring of name/login/email, joined to its SQSCP study.
// StudyCount is a correlated COUNT(DISTINCT id_study_lims) over the SAME
// study_users_mirror -> study_mirror join keyed by the candidate's (login, role)
// ONLY -- deliberately a DIFFERENT key from the (name, login, email, role) grouping
// (Note 2): in practice a (login, role) maps to one (name, email), but the two keys
// are kept distinct so the count is correct even when they diverge.
var resolvePersonUserBranch = `SELECT DISTINCT 'study_users' AS source, study_users_mirror.name AS name, ` +
	`study_users_mirror.login AS login, study_users_mirror.email AS email, study_users_mirror.role AS role, ` +
	`(SELECT COUNT(DISTINCT inner_study.id_study_lims) FROM study_users_mirror AS inner_users ` +
	`INNER JOIN study_mirror AS inner_study ON inner_study.id_study_tmp = inner_users.id_study_tmp ` +
	`WHERE inner_study.id_lims = 'SQSCP' AND inner_users.login = study_users_mirror.login ` +
	`AND inner_users.role = study_users_mirror.role) AS study_count ` +
	`FROM study_users_mirror INNER JOIN study_mirror ON study_mirror.id_study_tmp = study_users_mirror.id_study_tmp ` +
	`WHERE study_mirror.id_lims = 'SQSCP' AND ` + likeContainsClause(resolvePersonUserFields)

// resolvePersonCandidateUnion is the UNION ALL of the two candidate sources. Each
// branch is already DISTINCT within itself and the two carry disjoint source
// values, so no cross-source de-duplication is needed; the combined set is the
// distinct candidates across BOTH sources.
var resolvePersonCandidateUnion = resolvePersonSponsorBranch + ` UNION ALL ` + resolvePersonUserBranch

// resolvePersonPageSQL pages the combined candidate set, ordered by
// (source, name, login, role) for determinism and bounded by LIMIT/OFFSET. The
// faculty_sponsor branch's bound pattern is positionally first, then the
// study_users branch's three patterns, then LIMIT and OFFSET (see resolvePersonArgs).
var resolvePersonPageSQL = `SELECT source, name, login, email, role, study_count FROM (` +
	resolvePersonCandidateUnion + `) AS candidates ORDER BY source, name, login, role LIMIT ? OFFSET ?`

// resolvePersonCountSQL counts the rows ResolvePerson would return for the term: a
// COUNT(*) over the IDENTICAL UNION ALL of the two DISTINCT candidate branches with
// no LIMIT, so CountResolvePerson(term) equals len(ResolvePerson(term, all)) for any
// term.
var resolvePersonCountSQL = `SELECT COUNT(*) FROM (` + resolvePersonCandidateUnion + `) AS candidates`

// resolveStudyUsersRoles turns the raw, comma-separated role override into the
// lowercased role set to match exactly: an empty (whitespace-only) override yields
// the DEFAULT set, and a non-empty override is split on commas, each value trimmed
// and lowercased, OVERRIDING the default set. Blank entries (e.g. a trailing
// comma) are dropped; a non-empty override that reduces to no roles falls back to
// the default set so the IN-clause is never empty.
func resolveStudyUsersRoles(role string) []string {
	if strings.TrimSpace(role) == "" {
		return studyUsersDefaultRoles
	}

	roles := make([]string, 0, strings.Count(role, ",")+1)
	for _, candidate := range strings.Split(role, ",") {
		trimmed := strings.TrimSpace(candidate)
		if trimmed != "" {
			roles = append(roles, strings.ToLower(trimmed))
		}
	}
	if len(roles) == 0 {
		return studyUsersDefaultRoles
	}

	return roles
}

// studyUsersPageSQL is the role-filtered list query for person: SELECT DISTINCT
// the full study row plus the matched role, joined study_users_mirror -> study_mirror,
// de-duplicated to one row per (id_study_lims, role) and ordered by
// (id_study_lims, role) for stable pagination, bounded by LIMIT/OFFSET. The role
// IN-set is spliced in for the resolved roles.
func studyUsersPageSQL(roles []string) string {
	return `SELECT DISTINCT ` + studyUsersStudyColumns + `, study_users_mirror.role FROM ` + studyUsersJoin +
		studyUsersRoleClause(roles) +
		` ORDER BY study_mirror.id_study_lims, study_users_mirror.role LIMIT ? OFFSET ?`
}

// studyUsersCountSQL counts the rows StudiesForUser would return for person: a
// COUNT(*) over the same SELECT DISTINCT (id_study_lims, role) projection with no
// LIMIT (SQLite/MySQL have no COUNT(DISTINCT a, b)), so CountStudiesForUser equals
// len(StudiesForUser(person, role, all)) for any person/role.
func studyUsersCountSQL(roles []string) string {
	return `SELECT COUNT(*) FROM (SELECT DISTINCT study_mirror.id_study_lims, study_users_mirror.role FROM ` + studyUsersJoin +
		studyUsersRoleClause(roles) + `) AS distinct_user_studies`
}

// studyUsersRoleClause renders the `LOWER(study_users_mirror.role) IN (?, ?, ...)`
// predicate for the resolved role set, matching each role value EXACTLY and
// case-insensitively (the bound values are already lowercased by
// resolveStudyUsersRoles).
func studyUsersRoleClause(roles []string) string {
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(roles)), ", ")

	return ` AND LOWER(study_users_mirror.role) IN (` + placeholders + `)`
}

// studyUsersArgs builds the bound argument list shared by the list and count
// queries: the escaped person pattern once per substring field (name/login/email)
// followed by the lowercased role values for the IN-clause.
func studyUsersArgs(person string, roles []string) []any {
	args := likeContainsArgs(escapeLIKEPattern(person), studyUsersPersonFields)
	for _, role := range roles {
		args = append(args, role)
	}

	return args
}

// scanPersonStudyRow scans one StudiesForUser result row: the full study columns
// (via the shared studyScanTargets) followed by the matched study_users role.
func scanPersonStudyRow(scan func(dest ...any) error) (PersonStudy, error) {
	study := Study{}
	targets, apply := studyScanTargets(&study)

	var role string
	if err := scan(append(targets, &role)...); err != nil {
		return PersonStudy{}, err
	}
	apply()

	return PersonStudy{Study: study, Role: role}, nil
}

// personStudiesFromStudies wraps each full Study in a PersonStudy with an empty
// Role, the row shape the faculty-sponsor endpoint returns (the sponsor is not a
// study_users role).
func personStudiesFromStudies(studies []Study) []PersonStudy {
	rows := make([]PersonStudy, len(studies))
	for index, study := range studies {
		rows[index] = PersonStudy{Study: study}
	}

	return rows
}

// resolvePersonArgs builds the bound argument list shared by the list and count
// queries: the escaped term once for the faculty_sponsor branch, then once per
// study_users substring field (name/login/email), matching the positional order of
// the two UNION ALL branches.
func resolvePersonArgs(term string) []any {
	pattern := escapeLIKEPattern(term)

	args := likeContainsArgs(pattern, resolvePersonSponsorFields)

	return append(args, likeContainsArgs(pattern, resolvePersonUserFields)...)
}

// scanPersonCandidateRow scans one ResolvePerson result row into a PersonCandidate
// (source, name, login, email, role and the distinct study count).
func scanPersonCandidateRow(scan func(dest ...any) error) (PersonCandidate, error) {
	var candidate PersonCandidate
	if err := scan(&candidate.Source, &candidate.Name, &candidate.Login, &candidate.Email, &candidate.Role, &candidate.StudyCount); err != nil {
		return PersonCandidate{}, err
	}

	return candidate, nil
}

// StudiesForFacultySponsor returns the studies whose faculty_sponsor contains
// name (case-insensitive substring), each as a PersonStudy carrying the full
// Study and an empty Role (the sponsor is not a study_users role), ordered by
// id_study_lims for stable pagination and scoped to id_lims = 'SQSCP'. A
// whitespace-only name (after trimming) is rejected with ErrUnsupportedIdentifier
// (the HTTP handler 400s first; this re-validates defensively so a direct caller
// is not silently wrong). The people-endpoint cascade applies: a never-synced
// cache returns an empty slice joined with ErrCacheNeverSynced and ErrNotFound,
// while a synced cache with no match returns an empty slice and no error (there
// is no parent study to "not find").
func (c *Client) StudiesForFacultySponsor(ctx context.Context, name string, limit, offset int) ([]PersonStudy, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("%w: faculty sponsor name is required", ErrUnsupportedIdentifier)
	}

	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	args := append(likeContainsArgs(escapeLIKEPattern(name), facultySponsorField), limit, offset)

	studies, err := c.queryStudySearch(ctx, db, facultySponsorPageSQL, args...)
	if err != nil {
		return nil, err
	}
	if len(studies) > 0 {
		return personStudiesFromStudies(studies), nil
	}

	if err = c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		return []PersonStudy{}, err
	}

	return []PersonStudy{}, nil
}

// CountStudiesForFacultySponsor counts the studies StudiesForFacultySponsor would
// return for name: a COUNT(*) over the identical SQSCP / faculty_sponsor LIKE
// WHERE clause with no LIMIT, so the count equals
// len(StudiesForFacultySponsor(name, all)) for any name. Validation and the
// people-endpoint cascade mirror StudiesForFacultySponsor: a whitespace-only name
// is ErrUnsupportedIdentifier, a never-synced cache returns Count{} with both
// ErrCacheNeverSynced and ErrNotFound, and a synced cache with no match returns
// Count{Count: 0} and no error.
func (c *Client) CountStudiesForFacultySponsor(ctx context.Context, name string) (Count, error) {
	if strings.TrimSpace(name) == "" {
		return Count{}, fmt.Errorf("%w: faculty sponsor name is required", ErrUnsupportedIdentifier)
	}

	args := likeContainsArgs(escapeLIKEPattern(name), facultySponsorField)

	count, err := c.queryCount(ctx, facultySponsorCountSQL, "count studies for faculty sponsor", args...)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		return Count{}, err
	}

	return Count{Count: 0}, nil
}

// StudiesForUser returns the studies person is a study_users role member of, each
// as a PersonStudy carrying the full Study and the matched study_users role.
// person is matched case-insensitively as a substring of name, login OR email (so
// a login/email or a name resolves), joined to its study and scoped to
// id_lims = 'SQSCP'. role is the raw, comma-separated override of the default role
// set (owner, manager, data_access_contact): "" uses the default set and a
// non-empty role overrides it, matching each value exactly (case-insensitive). The
// same study may match under multiple roles, so results are de-duplicated to one
// row per (id_study_lims, role) and ordered by (id_study_lims, role) for stable
// pagination. A whitespace-only person (after trimming) is rejected with
// ErrUnsupportedIdentifier (the HTTP handler 400s first; this re-validates
// defensively). The people-endpoint cascade applies: a never-synced cache returns
// an empty slice joined with ErrCacheNeverSynced and ErrNotFound, while a synced
// cache with no match returns an empty slice and no error.
func (c *Client) StudiesForUser(ctx context.Context, person, role string, limit, offset int) ([]PersonStudy, error) {
	if strings.TrimSpace(person) == "" {
		return nil, fmt.Errorf("%w: person is required", ErrUnsupportedIdentifier)
	}

	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	roles := resolveStudyUsersRoles(role)
	args := append(studyUsersArgs(person, roles), limit, offset)

	rows, err := c.queryPersonStudies(ctx, db, studyUsersPageSQL(roles), args...)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		return rows, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		return []PersonStudy{}, err
	}

	return []PersonStudy{}, nil
}

// CountStudiesForUser counts the rows StudiesForUser would return for person and
// role: a COUNT(*) over the identical SELECT DISTINCT (id_study_lims, role)
// projection with no LIMIT, so the count equals len(StudiesForUser(person, role,
// all)) for any person/role. Validation and the people-endpoint cascade mirror
// StudiesForUser: a whitespace-only person is ErrUnsupportedIdentifier, a
// never-synced cache returns Count{} with both ErrCacheNeverSynced and
// ErrNotFound, and a synced cache with no match returns Count{Count: 0} and no
// error.
func (c *Client) CountStudiesForUser(ctx context.Context, person, role string) (Count, error) {
	if strings.TrimSpace(person) == "" {
		return Count{}, fmt.Errorf("%w: person is required", ErrUnsupportedIdentifier)
	}

	roles := resolveStudyUsersRoles(role)

	count, err := c.queryCount(ctx, studyUsersCountSQL(roles), "count studies for user", studyUsersArgs(person, roles)...)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		return Count{}, err
	}

	return Count{Count: 0}, nil
}

// ResolvePerson returns the DISTINCT candidate people matching term as a
// case-insensitive substring, from BOTH sources: study_mirror faculty_sponsor
// values (Source="faculty_sponsor", Name=the sponsor, StudyCount = distinct SQSCP
// studies for that sponsor) and study_users_mirror (name, login, email, role)
// tuples (Source="study_users", StudyCount = distinct studies for that candidate's
// (login, role)). Candidates are grouped by (name, login, email, role) but the
// study_users StudyCount is per (login, role) (Note 2). Results are ordered by
// (source, name, login, role) and scoped to id_lims = 'SQSCP'. A whitespace-only
// term (after trimming) is rejected with ErrUnsupportedIdentifier (the HTTP handler
// 400s first; this re-validates defensively). The people-endpoint cascade applies:
// a never-synced cache returns an empty slice joined with ErrCacheNeverSynced and
// ErrNotFound, while a synced cache with no match returns an empty slice and no
// error.
func (c *Client) ResolvePerson(ctx context.Context, term string, limit, offset int) ([]PersonCandidate, error) {
	if strings.TrimSpace(term) == "" {
		return nil, fmt.Errorf("%w: resolve-person term is required", ErrUnsupportedIdentifier)
	}

	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	args := append(resolvePersonArgs(term), limit, offset)

	candidates, err := c.queryPersonCandidates(ctx, db, resolvePersonPageSQL, args...)
	if err != nil {
		return nil, err
	}
	if len(candidates) > 0 {
		return candidates, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		return []PersonCandidate{}, err
	}

	return []PersonCandidate{}, nil
}

// CountResolvePerson counts the candidates ResolvePerson would return for term: a
// COUNT(*) over the identical UNION ALL of the two DISTINCT candidate branches with
// no LIMIT, so the count equals len(ResolvePerson(term, all)) for any term.
// Validation and the people-endpoint cascade mirror ResolvePerson: a whitespace-only
// term is ErrUnsupportedIdentifier, a never-synced cache returns Count{} with both
// ErrCacheNeverSynced and ErrNotFound, and a synced cache with no match returns
// Count{Count: 0} and no error.
func (c *Client) CountResolvePerson(ctx context.Context, term string) (Count, error) {
	if strings.TrimSpace(term) == "" {
		return Count{}, fmt.Errorf("%w: resolve-person term is required", ErrUnsupportedIdentifier)
	}

	count, err := c.queryCount(ctx, resolvePersonCountSQL, "count resolve person", resolvePersonArgs(term)...)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		return Count{}, err
	}

	return Count{Count: 0}, nil
}

// queryPersonCandidates runs the combined resolve-person query and scans each row
// into a PersonCandidate via scanPersonCandidateRow.
func (c *Client) queryPersonCandidates(ctx context.Context, db *sql.DB, query string, args ...any) ([]PersonCandidate, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: query resolve person: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	candidates := make([]PersonCandidate, 0)
	for rows.Next() {
		candidate, scanErr := scanPersonCandidateRow(rows.Scan)
		if scanErr != nil {
			return nil, fmt.Errorf("%w: scan resolve person: %w", ErrUpstreamImpaired, scanErr)
		}

		candidates = append(candidates, candidate)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query resolve person: %w", ErrUpstreamImpaired, err)
	}

	return candidates, nil
}

// queryPersonStudies runs the role-filtered StudiesForUser query and scans each
// row into a PersonStudy (full study plus the matched role) via scanPersonStudyRow.
func (c *Client) queryPersonStudies(ctx context.Context, db *sql.DB, query string, args ...any) ([]PersonStudy, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: query studies for user: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	personStudies := make([]PersonStudy, 0)
	for rows.Next() {
		personStudy, scanErr := scanPersonStudyRow(rows.Scan)
		if scanErr != nil {
			return nil, fmt.Errorf("%w: scan studies for user: %w", ErrUpstreamImpaired, scanErr)
		}

		personStudies = append(personStudies, personStudy)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query studies for user: %w", ErrUpstreamImpaired, err)
	}

	return personStudies, nil
}
