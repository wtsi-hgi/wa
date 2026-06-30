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

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wtsi-hgi/wa/mlwh"
)

// peopleNoMatchMessage is the neutral line shown when a synced cache returns no
// matching studies (`wa mlwh studies`) or no candidate people (`wa mlwh people`).
// It is distinct from the never-synced cache-unavailable message; both render
// cleanly and exit 0.
const peopleNoMatchMessage = "no matches"

var openMLWHStudiesClient = func(ctx context.Context, cfg mlwh.Config) (mlwhStudiesClient, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return mlwh.OpenCacheOnly(ctx, cfg.Cache)
	}

	return mlwh.Open(ctx, cfg)
}

var openMLWHStudiesRemoteClient = func(_ context.Context, cfg mlwh.RemoteConfig) (mlwhStudiesClient, error) {
	return mlwh.NewRemoteClient(cfg)
}

// studiesMode is the chosen `wa mlwh studies` mode: the named faculty sponsor or a
// study_users member.
type studiesMode int

const (
	studiesModeFacultySponsor studiesMode = iota
	studiesModeUser
)

func normaliseStudiesTotal(total, shown, offset int) int {
	if offset < 0 {
		offset = 0
	}

	if total < offset+shown {
		return offset + shown
	}

	return total
}

func writePersonStudiesHeader(out io.Writer, shown, total, offset int) {
	if shown == 0 {
		_, _ = fmt.Fprintf(out, "Studies (showing 0 of %d total):\n", total)

		return
	}

	if offset <= 0 && shown == total {
		_, _ = fmt.Fprintf(out, "Studies (%d total):\n", total)

		return
	}

	start := offset + 1
	if start < 1 {
		start = 1
	}

	end := offset + shown
	if end < shown {
		end = shown
	}

	_, _ = fmt.Fprintf(out, "Studies (showing %d-%d of %d total):\n", start, end, total)
}

func dispatchStudiesCount(ctx context.Context, client mlwhStudiesClient, mode studiesMode, query, role string) (mlwh.Count, error) {
	switch mode {
	case studiesModeFacultySponsor:
		return client.CountStudiesForFacultySponsor(ctx, query)
	case studiesModeUser:
		return client.CountStudiesForUser(ctx, query, role)
	default:
		return mlwh.Count{}, fmt.Errorf("unknown studies mode %d", mode)
	}
}

// mlwhStudiesClient is the subset of the MLWH query surface used by `wa mlwh
// studies` and `wa mlwh people`. Both *mlwh.RemoteClient and the local *mlwh.Client
// satisfy it.
type mlwhStudiesClient interface {
	StudiesForFacultySponsor(ctx context.Context, name string, limit, offset int) ([]mlwh.PersonStudy, error)
	CountStudiesForFacultySponsor(ctx context.Context, name string) (mlwh.Count, error)
	StudiesForUser(ctx context.Context, person, role string, limit, offset int) ([]mlwh.PersonStudy, error)
	CountStudiesForUser(ctx context.Context, person, role string) (mlwh.Count, error)
	ResolvePerson(ctx context.Context, term string, limit, offset int) ([]mlwh.PersonCandidate, error)
	Close() error
}

func openMLWHStudiesConfiguredClient(ctx context.Context, serverURL string) (mlwhStudiesClient, error) {
	if trimmedServerURL := strings.TrimSpace(serverURL); trimmedServerURL != "" {
		return openMLWHStudiesRemoteClient(ctx, mlwh.RemoteConfig{BaseURL: trimmedServerURL})
	}

	cfg, err := resolveMLWHInfoLocalConfig()
	if err != nil {
		return nil, err
	}

	client, err := openMLWHStudiesClient(ctx, cfg)
	if err != nil {
		if strings.TrimSpace(cfg.DSN) != "" && errors.Is(err, mlwh.ErrPasswordInDSN) {
			return nil, fmt.Errorf("WA_MLWH_DSN: %w", err)
		}

		return nil, err
	}

	return client, nil
}

func newMLWHStudiesCommand() *cobra.Command {
	var (
		serverURL      string
		facultySponsor string
		user           string
		role           string
		limit          int
		offset         int
		jsonOut        bool
	)

	command := &cobra.Command{
		Use:           "studies (--faculty-sponsor <name> | --user <login>)",
		Short:         "List the studies a person sponsors or is a role member of",
		SilenceUsage:  true,
		SilenceErrors: false,
		Long: strings.Join([]string{
			"List the studies associated with a person through a wa mlwh serve API,",
			"in one of two distinct modes. Exactly one is required:",
			"",
			"  --faculty-sponsor <name>   studies whose faculty_sponsor matches the",
			"                             named PI/sponsor (free-text, case-insensitive",
			"                             substring). The sponsor is not a study_users",
			"                             role, so these rows carry no role.",
			"  --user <login>             studies the person is a study_users role",
			"                             member of, matched case-insensitively across",
			"                             name, login and email. --role overrides the",
			"                             default role set (owner, manager,",
			"                             data_access_contact) with a comma-separated",
			"                             list, e.g. --role owner,manager. Each row",
			"                             surfaces its matched role.",
			"",
			"These two modes return different sets (the named sponsor versus role",
			"membership). Each text line carries id_study_lims, name and",
			"faculty_sponsor (plus role in --user mode); the total study count is",
			"printed. Use --limit/--offset to page and --json for a single JSON array",
			"of studies-by-person rows suitable for piping into jq.",
			"",
			"Normal CLI users should point this command at the MLWH query server",
			"with --server or WA_MLWH_SERVER_URL; database and cache credentials",
			"stay with the server process. When WA_ENV selects a scenario and no",
			"server URL is set, the command defaults to the active local MLWH API",
			"port from WA_*_SEQMETA_PORT. Operators can still run against a local",
			"cache with WA_MLWH_CACHE_PATH, or use WA_MLWH_DSN for direct local",
			"operator mode.",
			"",
			"Configuration is read from the environment. Use the persistent --env",
			"flag (or WA_ENV=development|test|production) to load matching",
			".env.<name> / .env.<name>.local files from the working directory",
			"before resolving:",
			"",
			"  WA_MLWH_SERVER_URL      Preferred. Base URL for wa mlwh serve.",
			"  WA_MLWH_BACKEND_URL     Lower-precedence compatibility default.",
			"  WA_*_SEQMETA_PORT       Scenario-local default API port.",
			"  WA_MLWH_DSN             Optional direct operator mode only.",
			"  WA_MLWH_PASSWORD        Optional. Password used with WA_MLWH_DSN.",
			"  WA_MLWH_CACHE_PATH      Optional local operator cache path or",
			"                          MySQL cache DSN without a password.",
			"  WA_MLWH_CACHE_PASSWORD  Optional. SQLCipher key used to encrypt",
			"                          the local cache when set.",
			"",
			"Examples:",
			"  # Studies sponsored by a named PI via a development stack",
			"  wa --env development mlwh studies --faculty-sponsor \"Carl Anderson\"",
			"",
			"  # Studies a user owns or manages from a remote MLWH server as JSON",
			"  wa mlwh studies --user ca3 --role owner,manager --server http://host:8091 --json",
			"",
			"  # A page of a user's studies against a local operator cache",
			"  WA_MLWH_CACHE_PATH=.tmp/mlwh-cache.sqlite wa mlwh studies --user ca3 --limit 50 --offset 50",
		}, "\n"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode, query, err := resolveStudiesMode(facultySponsor, user)
			if err != nil {
				return err
			}

			client, err := openMLWHStudiesConfiguredClient(cmd.Context(), serverURL)
			if err != nil {
				return fmt.Errorf("open mlwh client: %w", err)
			}
			defer func() { _ = client.Close() }()

			return runMLWHStudies(cmd.Context(), client, cmd.OutOrStdout(), mode, query, role, limit, offset, jsonOut)
		},
	}

	command.Flags().StringVar(&serverURL, "server", defaultMLWHInfoServerURL(), "MLWH server base URL (defaults to WA_MLWH_SERVER_URL, WA_MLWH_BACKEND_URL, or active WA_*_SEQMETA_PORT)")
	command.Flags().StringVar(&facultySponsor, "faculty-sponsor", "", "list studies whose faculty_sponsor matches this PI/sponsor name (mutually exclusive with --user)")
	command.Flags().StringVar(&user, "user", "", "list studies the person is a study_users role member of, by name, login or email (mutually exclusive with --faculty-sponsor)")
	command.Flags().StringVar(&role, "role", "", "with --user, comma-separated study_users roles overriding the default set (owner,manager,data_access_contact)")
	command.Flags().IntVar(&limit, "limit", 50, "maximum number of studies to return")
	command.Flags().IntVar(&offset, "offset", 0, "number of studies to skip (for pagination)")
	command.Flags().BoolVar(&jsonOut, "json", false, "emit a single JSON array of studies-by-person rows instead of human-readable text")

	return command
}

// resolveStudiesMode enforces that EXACTLY ONE of --faculty-sponsor/--user is given
// and returns the chosen mode plus its trimmed query value. Neither or both is a
// clear usage error (non-zero exit).
func resolveStudiesMode(facultySponsor, user string) (studiesMode, string, error) {
	sponsor := strings.TrimSpace(facultySponsor)
	person := strings.TrimSpace(user)

	switch {
	case sponsor != "" && person != "":
		return 0, "", errors.New("exactly one of --faculty-sponsor or --user is required (both were given)")
	case sponsor != "":
		return studiesModeFacultySponsor, sponsor, nil
	case person != "":
		return studiesModeUser, person, nil
	default:
		return 0, "", errors.New("exactly one of --faculty-sponsor or --user is required (neither was given)")
	}
}

// runMLWHStudies dispatches to the chosen mode's studies-by-person query and renders
// the result. It applies the soft-failure policy: a never-synced cache
// (ErrCacheNeverSynced) is a neutral cache-unavailable result (exit 0) and an empty
// list prints the neutral no-match line (exit 0).
func runMLWHStudies(ctx context.Context, client mlwhStudiesClient, out io.Writer, mode studiesMode, query, role string, limit, offset int, jsonOut bool) error {
	studies, err := dispatchStudiesMode(ctx, client, mode, query, role, limit, offset)
	if err != nil {
		if errors.Is(err, mlwh.ErrCacheNeverSynced) {
			return writeStudiesCacheUnavailable(out, jsonOut)
		}

		return fmt.Errorf("studies for %s: %w", query, err)
	}

	if jsonOut {
		return writePersonStudiesJSON(out, studies)
	}

	total, err := dispatchStudiesCount(ctx, client, mode, query, role)
	if err != nil {
		if errors.Is(err, mlwh.ErrCacheNeverSynced) {
			return writeStudiesCacheUnavailable(out, false)
		}

		return fmt.Errorf("count studies for %s: %w", query, err)
	}

	writePersonStudiesText(out, studies, total.Count, offset, mode == studiesModeUser)

	return nil
}

// writeStudiesCacheUnavailable renders the never-synced degradation: a neutral
// cache-unavailable message with no sync hint (text), or an empty JSON array
// (--json), so the never-synced case is indistinguishable in shape from a synced
// empty result. Exit 0 either way.
func writeStudiesCacheUnavailable(out io.Writer, jsonOut bool) error {
	if jsonOut {
		return writePersonStudiesJSON(out, nil)
	}

	_, _ = fmt.Fprintf(out, "%s\n", mlwhCacheUnavailableMessage)

	return nil
}

func writePersonStudiesJSON(out io.Writer, studies []mlwh.PersonStudy) error {
	if studies == nil {
		studies = []mlwh.PersonStudy{}
	}

	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(studies); err != nil {
		return fmt.Errorf("encode studies: %w", err)
	}

	return nil
}

// writePersonStudiesText renders the tabular text output: one line per study with
// its id_study_lims, name and faculty_sponsor; in user mode each line also carries
// its matched role. The total matching study count is printed separately from the
// shown page range. A genuinely empty result prints the neutral no-match line
// (exit 0).
func writePersonStudiesText(out io.Writer, studies []mlwh.PersonStudy, total, offset int, userMode bool) {
	total = normaliseStudiesTotal(total, len(studies), offset)
	if len(studies) == 0 && total == 0 {
		_, _ = fmt.Fprintf(out, "%s\n", peopleNoMatchMessage)

		return
	}

	writePersonStudiesHeader(out, len(studies), total, offset)
	for _, personStudy := range studies {
		_, _ = fmt.Fprintf(out, "  id_study_lims=%s name=%s faculty_sponsor=%s",
			personStudy.Study.IDStudyLims, personStudy.Study.Name, personStudy.Study.FacultySponsor)

		if userMode {
			_, _ = fmt.Fprintf(out, " role=%s", personStudy.Role)
		}

		_, _ = fmt.Fprintln(out)
	}
}

func dispatchStudiesMode(ctx context.Context, client mlwhStudiesClient, mode studiesMode, query, role string, limit, offset int) ([]mlwh.PersonStudy, error) {
	switch mode {
	case studiesModeFacultySponsor:
		return client.StudiesForFacultySponsor(ctx, query, limit, offset)
	case studiesModeUser:
		return client.StudiesForUser(ctx, query, role, limit, offset)
	default:
		return nil, fmt.Errorf("unknown studies mode %d", mode)
	}
}

func newMLWHPeopleCommand() *cobra.Command {
	var (
		serverURL string
		limit     int
		offset    int
		jsonOut   bool
	)

	command := &cobra.Command{
		Use:           "people <term>",
		Short:         "Resolve a partial person name to candidate sponsors and study_users",
		SilenceUsage:  true,
		SilenceErrors: false,
		Long: strings.Join([]string{
			"Resolve a partial or spoken person name to the distinct candidate people",
			"in the MLWH directory through a wa mlwh serve API, so you can disambiguate",
			"before running wa mlwh studies. The positional is the search term (matched",
			"case-insensitively).",
			"",
			"Candidates come from two sources: faculty_sponsor (the free-text named",
			"PI/sponsor; only name is populated) and study_users (the stored study",
			"membership identity: name, login, email and role). Each candidate carries",
			"a study_count of the distinct studies it covers. The same person can",
			"appear under both sources because they are different directories.",
			"",
			"Each text line carries source, name, login, email, role and study_count.",
			"Use --limit/--offset to page and --json for a single JSON array of",
			"candidates suitable for piping into jq.",
			"",
			"Normal CLI users should point this command at the MLWH query server",
			"with --server or WA_MLWH_SERVER_URL; database and cache credentials",
			"stay with the server process. When WA_ENV selects a scenario and no",
			"server URL is set, the command defaults to the active local MLWH API",
			"port from WA_*_SEQMETA_PORT. Operators can still run against a local",
			"cache with WA_MLWH_CACHE_PATH, or use WA_MLWH_DSN for direct local",
			"operator mode.",
			"",
			"Configuration is read from the environment. Use the persistent --env",
			"flag (or WA_ENV=development|test|production) to load matching",
			".env.<name> / .env.<name>.local files from the working directory",
			"before resolving:",
			"",
			"  WA_MLWH_SERVER_URL      Preferred. Base URL for wa mlwh serve.",
			"  WA_MLWH_BACKEND_URL     Lower-precedence compatibility default.",
			"  WA_*_SEQMETA_PORT       Scenario-local default API port.",
			"  WA_MLWH_DSN             Optional direct operator mode only.",
			"  WA_MLWH_PASSWORD        Optional. Password used with WA_MLWH_DSN.",
			"  WA_MLWH_CACHE_PATH      Optional local operator cache path or",
			"                          MySQL cache DSN without a password.",
			"  WA_MLWH_CACHE_PASSWORD  Optional. SQLCipher key used to encrypt",
			"                          the local cache when set.",
			"",
			"Examples:",
			"  # Resolve a partial name via a development stack started by make dev",
			"  wa --env development mlwh people carl",
			"",
			"  # Resolve a name from a remote MLWH server as JSON",
			"  wa mlwh people anderson --server http://host:8091 --json",
			"",
			"  # Resolve a name against a local operator cache",
			"  WA_MLWH_CACHE_PATH=.tmp/mlwh-cache.sqlite wa mlwh people ca3",
		}, "\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			term, err := parsePeopleTermArg(args)
			if err != nil {
				return err
			}

			client, err := openMLWHStudiesConfiguredClient(cmd.Context(), serverURL)
			if err != nil {
				return fmt.Errorf("open mlwh client: %w", err)
			}
			defer func() { _ = client.Close() }()

			return runMLWHPeople(cmd.Context(), client, cmd.OutOrStdout(), term, limit, offset, jsonOut)
		},
	}

	command.Flags().StringVar(&serverURL, "server", defaultMLWHInfoServerURL(), "MLWH server base URL (defaults to WA_MLWH_SERVER_URL, WA_MLWH_BACKEND_URL, or active WA_*_SEQMETA_PORT)")
	command.Flags().IntVar(&limit, "limit", 50, "maximum number of candidates to return")
	command.Flags().IntVar(&offset, "offset", 0, "number of candidates to skip (for pagination)")
	command.Flags().BoolVar(&jsonOut, "json", false, "emit a single JSON array of candidates instead of human-readable text")

	return command
}

// parsePeopleTermArg validates the single term positional and returns it trimmed. It
// rejects a missing/empty term with a clear usage error (non-zero exit).
func parsePeopleTermArg(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("usage: wa mlwh people <term>")
	}

	term := strings.TrimSpace(args[0])
	if term == "" {
		return "", errors.New("usage: wa mlwh people <term>")
	}

	return term, nil
}

// runMLWHPeople fetches the candidate people for term and renders the result. It
// applies the soft-failure policy: a never-synced cache (ErrCacheNeverSynced) is a
// neutral cache-unavailable result (exit 0) and an empty list prints the neutral
// no-match line (exit 0).
func runMLWHPeople(ctx context.Context, client mlwhStudiesClient, out io.Writer, term string, limit, offset int, jsonOut bool) error {
	candidates, err := client.ResolvePerson(ctx, term, limit, offset)
	if err != nil {
		if errors.Is(err, mlwh.ErrCacheNeverSynced) {
			return writePeopleCacheUnavailable(out, jsonOut)
		}

		return fmt.Errorf("resolve person %q: %w", term, err)
	}

	if jsonOut {
		return writePersonCandidatesJSON(out, candidates)
	}

	writePersonCandidatesText(out, candidates)

	return nil
}

// writePeopleCacheUnavailable renders the never-synced degradation: a neutral
// cache-unavailable message with no sync hint (text), or an empty JSON array
// (--json), so the never-synced case is indistinguishable in shape from a synced
// empty result. Exit 0 either way.
func writePeopleCacheUnavailable(out io.Writer, jsonOut bool) error {
	if jsonOut {
		return writePersonCandidatesJSON(out, nil)
	}

	_, _ = fmt.Fprintf(out, "%s\n", mlwhCacheUnavailableMessage)

	return nil
}

func writePersonCandidatesJSON(out io.Writer, candidates []mlwh.PersonCandidate) error {
	if candidates == nil {
		candidates = []mlwh.PersonCandidate{}
	}

	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(candidates); err != nil {
		return fmt.Errorf("encode candidates: %w", err)
	}

	return nil
}

// writePersonCandidatesText renders the tabular text output: one line per candidate
// with its source, name, login, email, role and study_count. An empty result prints
// the neutral no-match line (exit 0).
func writePersonCandidatesText(out io.Writer, candidates []mlwh.PersonCandidate) {
	if len(candidates) == 0 {
		_, _ = fmt.Fprintf(out, "%s\n", peopleNoMatchMessage)

		return
	}

	_, _ = fmt.Fprintf(out, "Candidates (%d):\n", len(candidates))
	for _, candidate := range candidates {
		_, _ = fmt.Fprintf(out, "  source=%s name=%s login=%s email=%s role=%s study_count=%d\n",
			candidate.Source, candidate.Name, candidate.Login, candidate.Email, candidate.Role, candidate.StudyCount)
	}
}
