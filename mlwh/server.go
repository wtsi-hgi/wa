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

package mlwh

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const mlwhServerFetchAllLimit = 1_000_000

// mlwhSearchDefaultLimit is the default page size for the substring-search
// endpoints when no limit query param is supplied. Unlike the other endpoints
// (which default to mlwhServerFetchAllLimit), search is its own pagination
// contract: a bounded default page with a hard maximum.
const mlwhSearchDefaultLimit = 100

// SearchMaxLimit is the maximum limit the substring-search endpoints accept. A
// larger limit is rejected with the bad_request 400 envelope rather than
// clamped, so callers cannot request unbounded search pages. It is exported so a
// client over-fetching search results (such as the results server's suggestion
// scan) can derive its own cap from this ceiling instead of duplicating the
// literal.
const SearchMaxLimit = 1000

// mlwhSearchMaxLimit is the unexported alias of SearchMaxLimit retained for
// internal references; it shares SearchMaxLimit's single source of truth so the
// enforced ceiling and the public symbol can never drift.
const mlwhSearchMaxLimit = SearchMaxLimit

// irodsPathsByFileTypeQueryer is the file-type-filter capability the iRODS list
// and count handlers need when a file_type query param is supplied. It is
// satisfied by *Client and *RemoteClient. Like samplesWithDataSinceQueryer, it is
// a narrow capability interface rather than a Queryer member because the filtered
// variants share the bare endpoints' single Registry entries (parameterised by
// the file_type query param), so they are not distinct endpoints on the query
// surface (preserving the 1:1 Method<->Registry invariant). A queryer that does
// not satisfy it never receives a file_type (the handler 400s an invalid one and
// otherwise only dispatches the filtered path when fileType != "").
type irodsPathsByFileTypeQueryer interface {
	IRODSPathsForStudyByFileType(ctx context.Context, studyLimsID, fileType string, limit, offset int) ([]IRODSPath, error)
	IRODSPathsForSampleByFileType(ctx context.Context, sangerName, fileType string, limit, offset int) ([]IRODSPath, error)
	CountIRODSPathsForStudyByFileType(ctx context.Context, studyLimsID, fileType string) (Count, error)
	CountIRODSPathsForSampleByFileType(ctx context.Context, sangerName, fileType string) (Count, error)
}

// Both the local Client and the RemoteClient provide the file-type-filtered iRODS
// variants, so a server over either dispatches a file_type query param to the
// filtered path.
var (
	_ irodsPathsByFileTypeQueryer = (*Client)(nil)
	_ irodsPathsByFileTypeQueryer = (*RemoteClient)(nil)
)

// irodsPathsForSampleResult dispatches the shared /sample/:id/irods list endpoint
// the same way as irodsPathsForStudyResult.
func irodsPathsForSampleResult(c *gin.Context, queryer Queryer, id, fileType string, pagination mlwhPagination) ([]IRODSPath, error) {
	if fileType != "" {
		if filtered, ok := queryer.(irodsPathsByFileTypeQueryer); ok {
			return filtered.IRODSPathsForSampleByFileType(c.Request.Context(), id, fileType, pagination.limit, pagination.offset)
		}
	}

	return queryer.IRODSPathsForSample(c.Request.Context(), id, pagination.limit, pagination.offset)
}

// countIRODSPathsForSampleResult dispatches the /sample/:id/irods/count endpoint
// and the sample list's total the same way as countIRODSPathsForStudyResult.
func countIRODSPathsForSampleResult(c *gin.Context, queryer Queryer, id, fileType string) (Count, error) {
	if fileType != "" {
		if filtered, ok := queryer.(irodsPathsByFileTypeQueryer); ok {
			return filtered.CountIRODSPathsForSampleByFileType(c.Request.Context(), id, fileType)
		}
	}

	return queryer.CountIRODSPathsForSample(c.Request.Context(), id)
}

// irodsPathsForStudyResult dispatches the shared /study/:id/irods list endpoint:
// with no file_type it returns the all-file-types IRODSPathsForStudy, and with a
// file_type (already normalised and validated by the handler) it returns the
// filtered IRODSPathsForStudyByFileType when the queryer supports it, falling
// back to the all-file-types list otherwise. Pagination is applied identically on
// both paths.
func irodsPathsForStudyResult(c *gin.Context, queryer Queryer, id, fileType string, pagination mlwhPagination) ([]IRODSPath, error) {
	if fileType != "" {
		if filtered, ok := queryer.(irodsPathsByFileTypeQueryer); ok {
			return filtered.IRODSPathsForStudyByFileType(c.Request.Context(), id, fileType, pagination.limit, pagination.offset)
		}
	}

	return queryer.IRODSPathsForStudy(c.Request.Context(), id, pagination.limit, pagination.offset)
}

// countIRODSPathsForStudyResult dispatches the /study/:id/irods/count and (when
// it sizes the list) the list endpoint's total: with no file_type it returns the
// all-file-types count, and with a file_type it returns the filtered count when
// the queryer supports it, so X-Total-Count tracks the filtered list and the two
// cannot drift.
func countIRODSPathsForStudyResult(c *gin.Context, queryer Queryer, id, fileType string) (Count, error) {
	if fileType != "" {
		if filtered, ok := queryer.(irodsPathsByFileTypeQueryer); ok {
			return filtered.CountIRODSPathsForStudyByFileType(c.Request.Context(), id, fileType)
		}
	}

	return queryer.CountIRODSPathsForStudy(c.Request.Context(), id)
}

// Server serves the MLWH read/query REST API.
type Server struct {
	queryer Queryer
}

// NewServer constructs an MLWH API server.
func NewServer(q Queryer, opts ...ServerOption) *Server {
	server := &Server{queryer: q}

	for _, opt := range opts {
		if opt != nil {
			opt(server)
		}
	}

	return server
}

// RegisterRoutes registers MLWH API routes on the provided Gin routers. The
// plain router carries the operational plain routes (GET /health and GET
// /openapi.json), and, when no auth group is supplied, the Registry endpoints
// at their root paths (unauthenticated mode). When an auth group is supplied
// (secured mode) the Registry endpoints register behind it while /health and
// /openapi.json stay plain routes on the router, so readiness checks and the
// OpenAPI document remain reachable and unauthenticated.
func (s *Server) RegisterRoutes(router *gin.Engine, auth *gin.RouterGroup) {
	if s == nil {
		return
	}

	if router != nil {
		configureMLWHRouter(router)
		registerMLWHPlainRoutes(router)

		if auth == nil {
			registerMLWHEndpoints(router, s.queryer)
		}
	}

	if auth != nil {
		registerMLWHEndpoints(auth, s.queryer)
	}
}

func configureMLWHRouter(router *gin.Engine) {
	router.UseRawPath = true
	router.UnescapePathValues = false
}

// registerMLWHPlainRoutes registers the operational routes that are not Registry
// (Queryer) endpoints: GET /health, a cheap liveness probe that returns
// {"status":"ok"} without consulting the queryer, so readiness checks stay
// inexpensive and never surface a never-synced 503; and GET /openapi.json, the
// self-describing OpenAPI 3.1.0 document. Both are plain routes, so in secured
// mode they stay on the unauthenticated router and remain reachable without a
// token.
func registerMLWHPlainRoutes(router *gin.Engine) {
	router.GET("/health", mlwhHealthHandler)
	router.GET("/openapi.json", mlwhOpenAPIHandler)
}

func registerMLWHEndpoints(registrar mlwhRouteRegistrar, queryer Queryer) {
	for _, entry := range Registry {
		registrar.Handle(entry.Verb, entry.Path, mlwhEndpointHandler(queryer, entry.Method))
	}
}

// ServerOption configures a Server.
type ServerOption func(*Server)

type mlwhRouteRegistrar interface {
	Handle(string, string, ...gin.HandlerFunc) gin.IRoutes
}

type mlwhPagination struct {
	limit  int
	offset int
}

func mlwhIDAndPagination(c *gin.Context) (string, mlwhPagination, bool) {
	id, ok := mlwhPathParam(c, "id")
	if !ok {
		return "", mlwhPagination{}, false
	}

	pagination, ok := mlwhPaginationFromQuery(c)
	if !ok {
		return "", mlwhPagination{}, false
	}

	return id, pagination, true
}

func mlwhLibraryStudyAndPagination(c *gin.Context) (string, string, mlwhPagination, bool) {
	pipeline, study, ok := mlwhLibraryStudy(c)
	if !ok {
		return "", "", mlwhPagination{}, false
	}

	pagination, ok := mlwhPaginationFromQuery(c)
	if !ok {
		return "", "", mlwhPagination{}, false
	}

	return pipeline, study, pagination, true
}

func mlwhPaginationFromQuery(c *gin.Context) (mlwhPagination, bool) {
	limit, ok := mlwhQueryInt(c, "limit", mlwhServerFetchAllLimit)
	if !ok {
		return mlwhPagination{}, false
	}
	if limit < 0 {
		writeMLWHBadRequest(c, "limit must not be negative")

		return mlwhPagination{}, false
	}

	offset, ok := mlwhQueryInt(c, "offset", 0)
	if !ok {
		return mlwhPagination{}, false
	}
	if offset < 0 {
		writeMLWHBadRequest(c, "offset must not be negative")

		return mlwhPagination{}, false
	}

	return mlwhPagination{limit: limit, offset: offset}, true
}

// mlwhTermAndSearchPagination reads the :term path param and the search-specific
// pagination (default limit mlwhSearchDefaultLimit, maximum mlwhSearchMaxLimit).
// A non-integer or over-maximum limit aborts with the bad_request 400 envelope
// before the queryer is reached, leaving the non-search fetch-all default
// untouched for every other endpoint.
func mlwhTermAndSearchPagination(c *gin.Context) (string, mlwhPagination, bool) {
	term, ok := mlwhPathParam(c, "term")
	if !ok {
		return "", mlwhPagination{}, false
	}

	pagination, ok := mlwhSearchPaginationFromQuery(c)
	if !ok {
		return "", mlwhPagination{}, false
	}

	return term, pagination, true
}

func mlwhSearchPaginationFromQuery(c *gin.Context) (mlwhPagination, bool) {
	limit, ok := mlwhQueryInt(c, "limit", mlwhSearchDefaultLimit)
	if !ok {
		return mlwhPagination{}, false
	}
	if limit < 0 {
		writeMLWHBadRequest(c, "limit must not be negative")

		return mlwhPagination{}, false
	}
	if limit > mlwhSearchMaxLimit {
		writeMLWHBadRequest(c, fmt.Sprintf("limit must not exceed %d", mlwhSearchMaxLimit))

		return mlwhPagination{}, false
	}

	offset, ok := mlwhQueryInt(c, "offset", 0)
	if !ok {
		return mlwhPagination{}, false
	}
	if offset < 0 {
		writeMLWHBadRequest(c, "offset must not be negative")

		return mlwhPagination{}, false
	}

	return mlwhPagination{limit: limit, offset: offset}, true
}

func mlwhEndpointHandler(queryer Queryer, method string) gin.HandlerFunc {
	switch method {
	case "ClassifyIdentifier":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.ClassifyIdentifier(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "ResolveSample":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.ResolveSample(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "ResolveSampleName":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.ResolveSampleName(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "ResolveStudy":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.ResolveStudy(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "ResolveRun":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.ResolveRun(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "ResolveLibrary":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.ResolveLibrary(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "ResolveLibraryIdentifier":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.ResolveLibraryIdentifier(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "AllStudies":
		return func(c *gin.Context) {
			pagination, ok := mlwhPaginationFromQuery(c)
			if !ok {
				return
			}
			ctx := c.Request.Context()
			result, err := queryer.AllStudies(ctx, pagination.limit, pagination.offset)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(queryer.CountStudies(ctx))
			})
		}
	case "SamplesForStudy":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			ctx := c.Request.Context()
			result, err := queryer.SamplesForStudy(ctx, id, pagination.limit, pagination.offset)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(queryer.CountSamplesForStudy(ctx, id))
			})
		}
	case "SamplesForRun":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			ctx := c.Request.Context()
			result, err := queryer.SamplesForRun(ctx, id, pagination.limit, pagination.offset)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(queryer.CountSamplesForRun(ctx, id))
			})
		}
	case "SamplesForLibrary":
		return func(c *gin.Context) {
			pipeline, study, pagination, ok := mlwhLibraryStudyAndPagination(c)
			if !ok {
				return
			}
			ctx := c.Request.Context()
			result, err := queryer.SamplesForLibrary(ctx, pipeline, study, pagination.limit, pagination.offset)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(queryer.CountSamplesForLibrary(ctx, pipeline, study))
			})
		}
	case "SamplesForLibraryID":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			ctx := c.Request.Context()
			result, err := queryer.SamplesForLibraryID(ctx, id, pagination.limit, pagination.offset)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(queryer.CountSamplesForLibraryID(ctx, id))
			})
		}
	case "SamplesForLibraryLimsID":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			ctx := c.Request.Context()
			result, err := queryer.SamplesForLibraryLimsID(ctx, id, pagination.limit, pagination.offset)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(queryer.CountSamplesForLibraryLimsID(ctx, id))
			})
		}
	case "SamplesForLibraryType":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			ctx := c.Request.Context()
			result, err := queryer.SamplesForLibraryType(ctx, id, pagination.limit, pagination.offset)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(queryer.CountSamplesForLibraryType(ctx, id))
			})
		}
	case "LibrariesForStudy":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			ctx := c.Request.Context()
			result, err := queryer.LibrariesForStudy(ctx, id, pagination.limit, pagination.offset)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(queryer.CountLibrariesForStudy(ctx, id))
			})
		}
	case "RunsForStudy":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			ctx := c.Request.Context()
			result, err := queryer.RunsForStudy(ctx, id, pagination.limit, pagination.offset)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(queryer.CountRunsForStudy(ctx, id))
			})
		}
	case "StudyOverview":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.StudyOverview(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "RunOverview":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.RunOverview(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "RunStatus":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.RunStatus(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "SampleProgress":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.SampleProgress(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "StatusBreakdown":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.StatusBreakdown(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "SamplesWithData":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			since, until, ok := mlwhAddedWindowFromQuery(c)
			if !ok {
				return
			}
			result, err := samplesWithDataResult(c, queryer, id, since, until, pagination)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(countSamplesWithDataResult(c, queryer, id, since, until))
			})
		}
	case "SamplesWithoutData":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			ctx := c.Request.Context()
			result, err := queryer.SamplesWithoutData(ctx, id, pagination.limit, pagination.offset)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countSamplesWithoutData(ctx, queryer, id)
			})
		}
	case "LanesForSample":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			ctx := c.Request.Context()
			result, err := queryer.LanesForSample(ctx, id, pagination.limit, pagination.offset)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(queryer.CountLanesForSample(ctx, id))
			})
		}
	case "IRODSPathsForSample":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			fileType, ok := mlwhFileTypeFromQuery(c)
			if !ok {
				return
			}
			result, err := irodsPathsForSampleResult(c, queryer, id, fileType, pagination)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(countIRODSPathsForSampleResult(c, queryer, id, fileType))
			})
		}
	case "IRODSPathsForStudy":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			fileType, ok := mlwhFileTypeFromQuery(c)
			if !ok {
				return
			}
			result, err := irodsPathsForStudyResult(c, queryer, id, fileType, pagination)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(countIRODSPathsForStudyResult(c, queryer, id, fileType))
			})
		}
	case "IRODSPathsForRun":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			fileType, ok := mlwhFileTypeFromQuery(c)
			if !ok {
				return
			}
			ctx := c.Request.Context()
			result, err := queryer.IRODSPathsForRun(ctx, id, fileType, pagination.limit, pagination.offset)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(queryer.CountIRODSPathsForRun(ctx, id, fileType))
			})
		}
	case "StudyManifest":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			fileType, ok := mlwhFileTypeFromQuery(c)
			if !ok {
				return
			}
			withIRODS, ok := mlwhQueryBool(c, "with_irods")
			if !ok {
				return
			}
			ctx := c.Request.Context()
			result, err := queryer.StudyManifest(ctx, id, fileType, withIRODS, pagination.limit, pagination.offset)
			writeMLWHStudyManifest(c, result, err, pagination.offset, func() (int, error) {
				return studyManifestTotal(ctx, queryer, id)
			})
		}
	case "StudiesForSample":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.StudiesForSample(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "StudiesForFacultySponsor":
		return func(c *gin.Context) {
			name, ok := mlwhPeopleName(c, "name")
			if !ok {
				return
			}
			pagination, ok := mlwhSearchPaginationFromQuery(c)
			if !ok {
				return
			}
			ctx := c.Request.Context()
			result, err := queryer.StudiesForFacultySponsor(ctx, name, pagination.limit, pagination.offset)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(queryer.CountStudiesForFacultySponsor(ctx, name))
			})
		}
	case "CountStudiesForFacultySponsor":
		return func(c *gin.Context) {
			name, ok := mlwhPeopleName(c, "name")
			if !ok {
				return
			}
			result, err := queryer.CountStudiesForFacultySponsor(c.Request.Context(), name)
			writeMLWHResult(c, result, err)
		}
	case "StudiesForUser":
		return func(c *gin.Context) {
			person, ok := mlwhPeopleName(c, "person")
			if !ok {
				return
			}
			pagination, ok := mlwhSearchPaginationFromQuery(c)
			if !ok {
				return
			}
			role := c.Query("role")
			ctx := c.Request.Context()
			result, err := queryer.StudiesForUser(ctx, person, role, pagination.limit, pagination.offset)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(queryer.CountStudiesForUser(ctx, person, role))
			})
		}
	case "CountStudiesForUser":
		return func(c *gin.Context) {
			person, ok := mlwhPeopleName(c, "person")
			if !ok {
				return
			}
			result, err := queryer.CountStudiesForUser(c.Request.Context(), person, c.Query("role"))
			writeMLWHResult(c, result, err)
		}
	case "ResolvePerson":
		return func(c *gin.Context) {
			term, ok := mlwhPeopleName(c, "term")
			if !ok {
				return
			}
			pagination, ok := mlwhSearchPaginationFromQuery(c)
			if !ok {
				return
			}
			ctx := c.Request.Context()
			result, err := queryer.ResolvePerson(ctx, term, pagination.limit, pagination.offset)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(queryer.CountResolvePerson(ctx, term))
			})
		}
	case "CountResolvePerson":
		return func(c *gin.Context) {
			term, ok := mlwhPeopleName(c, "term")
			if !ok {
				return
			}
			result, err := queryer.CountResolvePerson(c.Request.Context(), term)
			writeMLWHResult(c, result, err)
		}
	case "FindSamplesBySangerID":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.FindSamplesBySangerID(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "FindSamplesByIDSampleLims":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.FindSamplesByIDSampleLims(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "FindSamplesByAccessionNumber":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.FindSamplesByAccessionNumber(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "FindSamplesBySupplierName":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.FindSamplesBySupplierName(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "FindSamplesByLibraryType":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.FindSamplesByLibraryType(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "ExpandIdentifier":
		return func(c *gin.Context) {
			kind, id, ok := mlwhKindAndID(c)
			if !ok {
				return
			}
			result, err := queryer.ExpandIdentifier(c.Request.Context(), kind, id)
			writeMLWHResult(c, result, err)
		}
	case "ExpandSearchValues":
		return func(c *gin.Context) {
			kind, id, ok := mlwhKindAndID(c)
			if !ok {
				return
			}
			result, err := queryer.ExpandSearchValues(c.Request.Context(), kind, id)
			writeMLWHResult(c, result, err)
		}
	case "ExpandSampleSearchValues":
		return func(c *gin.Context) {
			kind, id, ok := mlwhKindAndID(c)
			if !ok {
				return
			}
			result, err := queryer.ExpandSampleSearchValues(c.Request.Context(), kind, id)
			writeMLWHResult(c, result, err)
		}
	case "Enrich":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.Enrich(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "SampleDetail":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.SampleDetail(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "StudyDetail":
		return func(c *gin.Context) {
			id, opts, ok := mlwhDetailIDAndOptions(c)
			if !ok {
				return
			}
			writeMLWHStudyDetail(c, queryer, id, opts)
		}
	case "RunDetail":
		return func(c *gin.Context) {
			id, opts, ok := mlwhDetailIDAndOptions(c)
			if !ok {
				return
			}
			writeMLWHRunDetail(c, queryer, id, opts)
		}
	case "LibraryDetail":
		return func(c *gin.Context) {
			pipeline, study, ok := mlwhLibraryStudy(c)
			if !ok {
				return
			}
			result, err := queryer.LibraryDetail(c.Request.Context(), pipeline, study)
			writeMLWHResult(c, result, err)
		}
	case "SearchStudies":
		return func(c *gin.Context) {
			term, pagination, ok := mlwhTermAndSearchPagination(c)
			if !ok {
				return
			}
			ctx := c.Request.Context()
			result, err := queryer.SearchStudies(ctx, term, pagination.limit, pagination.offset)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(queryer.CountStudySearch(ctx, term))
			})
		}
	case "SearchSamples":
		return func(c *gin.Context) {
			term, pagination, ok := mlwhTermAndSearchPagination(c)
			if !ok {
				return
			}
			ctx := c.Request.Context()
			result, err := queryer.SearchSamples(ctx, term, pagination.limit, pagination.offset)
			writeMLWHPaginatedResult(c, result, err, pagination.offset, func() (int, error) {
				return countValue(queryer.CountSampleSearch(ctx, term))
			})
		}
	case "CountStudySearch":
		return func(c *gin.Context) {
			term, ok := mlwhPathParam(c, "term")
			if !ok {
				return
			}
			result, err := queryer.CountStudySearch(c.Request.Context(), term)
			writeMLWHResult(c, result, err)
		}
	case "CountSampleSearch":
		return func(c *gin.Context) {
			term, ok := mlwhPathParam(c, "term")
			if !ok {
				return
			}
			result, err := queryer.CountSampleSearch(c.Request.Context(), term)
			writeMLWHResult(c, result, err)
		}
	case "CountStudies":
		return func(c *gin.Context) {
			result, err := queryer.CountStudies(c.Request.Context())
			writeMLWHResult(c, result, err)
		}
	case "CountSamplesForStudy":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.CountSamplesForStudy(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "CountSamplesWithData":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			since, until, ok := mlwhAddedWindowFromQuery(c)
			if !ok {
				return
			}
			result, err := countSamplesWithDataResult(c, queryer, id, since, until)
			writeMLWHResult(c, result, err)
		}
	case "CountSamplesForRun":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.CountSamplesForRun(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "CountSamplesForLibrary":
		return func(c *gin.Context) {
			pipeline, study, ok := mlwhLibraryStudy(c)
			if !ok {
				return
			}
			result, err := queryer.CountSamplesForLibrary(c.Request.Context(), pipeline, study)
			writeMLWHResult(c, result, err)
		}
	case "CountSamplesForLibraryID":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.CountSamplesForLibraryID(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "CountSamplesForLibraryLimsID":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.CountSamplesForLibraryLimsID(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "CountSamplesForLibraryType":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.CountSamplesForLibraryType(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "CountRunsForStudy":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.CountRunsForStudy(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "CountStudyManifest":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.CountStudyManifest(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "CountLibrariesForStudy":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.CountLibrariesForStudy(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "CountLanesForSample":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.CountLanesForSample(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "CountIRODSPathsForSample":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			fileType, ok := mlwhFileTypeFromQuery(c)
			if !ok {
				return
			}
			result, err := countIRODSPathsForSampleResult(c, queryer, id, fileType)
			writeMLWHResult(c, result, err)
		}
	case "CountIRODSPathsForStudy":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			fileType, ok := mlwhFileTypeFromQuery(c)
			if !ok {
				return
			}
			result, err := countIRODSPathsForStudyResult(c, queryer, id, fileType)
			writeMLWHResult(c, result, err)
		}
	case "CountIRODSPathsForRun":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			fileType, ok := mlwhFileTypeFromQuery(c)
			if !ok {
				return
			}
			result, err := queryer.CountIRODSPathsForRun(c.Request.Context(), id, fileType)
			writeMLWHResult(c, result, err)
		}
	case "CountFindSamplesBySangerID":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.CountFindSamplesBySangerID(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "CountFindSamplesByIDSampleLims":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.CountFindSamplesByIDSampleLims(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "CountFindSamplesByAccessionNumber":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.CountFindSamplesByAccessionNumber(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "CountFindSamplesBySupplierName":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.CountFindSamplesBySupplierName(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "CountFindSamplesByLibraryType":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.CountFindSamplesByLibraryType(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "Freshness":
		return func(c *gin.Context) {
			result, err := queryer.Freshness(c.Request.Context())
			writeMLWHResult(c, result, err)
		}
	default:
		panic(fmt.Sprintf("mlwh: no endpoint handler for %s", method))
	}
}

func mlwhPathParam(c *gin.Context, name string) (string, bool) {
	value, err := url.PathUnescape(c.Param(name))
	if err != nil {
		writeMLWHBadRequest(c, "invalid path parameter "+name)

		return "", false
	}

	return value, true
}

func writeMLWHBadRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, httpErrorEnvelope{
		Code:    "bad_request",
		Message: message,
	})
}

func writeMLWHResult(c *gin.Context, result any, err error) {
	if err != nil {
		writeMLWHError(c, err)

		return
	}

	c.JSON(http.StatusOK, result)
}

func writeMLWHError(c *gin.Context, err error) {
	status, code := httpStatusAndErrorCode(err)
	c.JSON(status, httpErrorEnvelope{
		Code:    code,
		Message: err.Error(),
	})
}

// writeMLWHPaginatedResult writes a paginated list response: on error it writes
// the error envelope and sets no sizing headers, and on success it sets the
// X-Total-Count / X-Next-Offset list-sizing headers (via writeListSizingHeaders)
// before writing the bare JSON-array body. total resolves the total matching
// rows by reusing the list's /count counterpart (so X-Total-Count equals that
// count endpoint and the two cannot drift); offset is the request offset and the
// page length is len(items). A total-resolution error leaves the body intact and
// merely omits the headers, so a successful list never degrades into a 500.
func writeMLWHPaginatedResult[T any](c *gin.Context, items []T, err error, offset int, total func() (int, error)) {
	if err != nil {
		writeMLWHError(c, err)

		return
	}

	if totalRows, totalErr := total(); totalErr == nil {
		writeListSizingHeaders(c, totalRows, offset, len(items))
	}

	c.JSON(http.StatusOK, items)
}

// writeListSizingHeaders sets the list-sizing response headers shared by every
// paginated list endpoint (spec M): X-Total-Count is the total matching rows and
// X-Next-Offset is the offset of the next page (offset+returned when more rows
// remain, i.e. offset+returned < total, else -1 for the last page). It is the
// single reusable header path; /run/:id/detail and /study/:id/detail (their
// nested collections being paginated the same way) call it too, so the header
// logic lives in one place. The response body stays a bare JSON array.
func writeListSizingHeaders(c *gin.Context, total, offset, returned int) {
	c.Header("X-Total-Count", strconv.Itoa(total))

	nextOffset := offset + returned
	if nextOffset >= total {
		nextOffset = -1
	}

	c.Header("X-Next-Offset", strconv.Itoa(nextOffset))
}

// countValue adapts a (Count, error) count-method result to the (int, error)
// total resolver writeMLWHPaginatedResult expects, so each paginated list sizes
// itself by reusing its /count counterpart's value (the X-Total-Count header
// then equals that count endpoint and the two cannot drift).
func countValue(count Count, err error) (int, error) {
	if err != nil {
		return 0, err
	}

	return count.Count, nil
}

// mlwhAddedWindowFromQuery reads the optional since/until RFC3339 query params of
// the windowed samples-with-data count and list. A malformed since or until, or an
// until supplied WITHOUT a since, aborts with the bad_request 400 envelope BEFORE
// the queryer is reached, so junk never reaches the query layer; an absent bound is
// returned as an empty string (the all-time / open-ended case). until is only the
// upper bound of a [since, until) window, so until-without-since is rejected rather
// than silently dropped (which would return the all-time superset). The raw RFC3339
// strings are passed through on success so the query layer normalises them once.
func mlwhAddedWindowFromQuery(c *gin.Context) (string, string, bool) {
	since, ok := mlwhQueryRFC3339(c, "since")
	if !ok {
		return "", "", false
	}

	until, ok := mlwhQueryRFC3339(c, "until")
	if !ok {
		return "", "", false
	}

	if since == "" && until != "" {
		writeMLWHBadRequest(c, "invalid until: requires since")

		return "", "", false
	}

	return since, until, true
}

// mlwhQueryRFC3339 reads a query param that, when present, must be an RFC3339
// timestamp. An empty value is returned unchanged (the param is optional); a
// present-but-malformed value aborts with the bad_request 400 envelope and
// reports false, so the handler returns before reaching the queryer.
func mlwhQueryRFC3339(c *gin.Context, name string) (string, bool) {
	raw := c.Query(name)
	if raw == "" {
		return "", true
	}

	if _, err := time.Parse(time.RFC3339, raw); err != nil {
		writeMLWHBadRequest(c, "invalid "+name+": must be an RFC3339 timestamp")

		return "", false
	}

	return raw, true
}

// countSamplesWithoutData resolves the total for the samples-without-data list.
// There is no dedicated /count endpoint for it; per the partition contract
// (with_data + without_data = samples_total) it is samples_total minus
// samples_with_data, reusing the two existing study counts so the figure cannot
// drift from them. An error from either count is propagated so the caller omits
// the sizing headers rather than reporting a wrong total.
func countSamplesWithoutData(ctx context.Context, queryer Queryer, studyLimsID string) (int, error) {
	total, err := countValue(queryer.CountSamplesForStudy(ctx, studyLimsID))
	if err != nil {
		return 0, err
	}

	withData, err := countValue(queryer.CountSamplesWithData(ctx, studyLimsID))
	if err != nil {
		return 0, err
	}

	return total - withData, nil
}

// mlwhFileTypeFromQuery reads the optional file_type query param of the iRODS
// list and count endpoints. It mirrors mlwhQueryRFC3339's contract: an ABSENT
// param is returned as ("", true) (the all-file-types case, the same as the bare
// endpoint); a PRESENT param is normalised (strip one leading '.', lowercase) and
// validated, and an empty/whitespace value or one containing a LIKE wildcard
// ('%'/'_') or path separator ('/') aborts with the bad_request 400 envelope
// BEFORE the queryer is reached, reporting false. A present-and-valid value is
// returned normalised. The present-but-empty case is a 400 (not silently treated
// as no filter), so `?file_type=` is rejected while an omitted param is allowed.
func mlwhFileTypeFromQuery(c *gin.Context) (string, bool) {
	raw, present := c.GetQuery("file_type")
	if !present {
		return "", true
	}

	normalised, err := normaliseFileType(raw)
	if err != nil || normalised == "" {
		writeMLWHBadRequest(c, "invalid file_type: must be a non-empty file suffix without '%', '_' or '/'")

		return "", false
	}

	return normalised, true
}

// writeMLWHStudyManifest writes the study manifest envelope: on error it writes
// the error envelope and sets no headers, and on success it sets the
// X-Total-Count / X-Next-Offset list-sizing headers (sizing the paginated Rows
// collection by the study's product count, the C2 count grain) before writing the
// envelope body. The body stays the full StudyManifest object (not a bare array),
// so unlike the bare-slice lists it cannot reuse writeMLWHPaginatedResult; the
// header path is the same writeListSizingHeaders, so the manifest's sizing matches
// every other paginated endpoint. A total error leaves the headers unset rather
// than reporting a wrong total.
func writeMLWHStudyManifest(c *gin.Context, manifest StudyManifest, err error, offset int, total func() (int, error)) {
	if err != nil {
		writeMLWHError(c, err)

		return
	}

	if totalRows, totalErr := total(); totalErr == nil {
		writeListSizingHeaders(c, totalRows, offset, len(manifest.Rows))
	}

	c.JSON(http.StatusOK, manifest)
}

// studyManifestTotal resolves the total product count sizing the manifest's Rows
// collection. The manifest is product-grained, so the total is the distinct
// (id_run, position, tag_index) products in the study, the same grain the C2
// /study/:id/manifest/count endpoint counts; a queryer that exposes that count
// (the Client) reports it so X-Total-Count equals the count endpoint and the two
// cannot drift, and a queryer that does not (a test fake) reports 0 so the body is
// still served.
func studyManifestTotal(ctx context.Context, queryer Queryer, studyLimsID string) (int, error) {
	counter, ok := queryer.(studyManifestCounter)
	if !ok {
		return 0, nil
	}

	return counter.countStudyManifestProducts(ctx, studyLimsID)
}

// mlwhPeopleName reads a people-endpoint path param (e.g. the faculty-sponsor
// :name) and rejects a whitespace-only value with the bad_request 400 envelope
// BEFORE the queryer is reached, reporting false, so an empty/whitespace term
// never reaches the query layer. The raw (untrimmed) value is returned on
// success: the people queries match the term as a case-insensitive substring, so
// surrounding whitespace is matched literally rather than stripped, and only the
// whitespace-only case is a 400. The Client method re-validates the same way
// defensively (returning ErrUnsupportedIdentifier) so a direct caller is not
// silently wrong.
func mlwhPeopleName(c *gin.Context, name string) (string, bool) {
	value, ok := mlwhPathParam(c, name)
	if !ok {
		return "", false
	}

	if strings.TrimSpace(value) == "" {
		writeMLWHBadRequest(c, "invalid "+name+": must not be empty or whitespace")

		return "", false
	}

	return value, true
}

func mlwhKindAndID(c *gin.Context) (IdentifierKind, string, bool) {
	kind, ok := mlwhPathParam(c, "kind")
	if !ok {
		return "", "", false
	}

	id, ok := mlwhPathParam(c, "id")
	if !ok {
		return "", "", false
	}

	return IdentifierKind(kind), id, true
}

// writeMLWHStudyDetail serves a study detail: it sets the X-Total-Count /
// X-Next-Offset list-sizing headers from the full nested sample count (via item
// E2's writeListSizingHeaders) before writing the de-duplicated body. A queryer
// without the options capability falls back to the plain, all-rows detail with no
// sizing headers.
func writeMLWHStudyDetail(c *gin.Context, queryer Queryer, id string, opts detailOptions) {
	withOptions, ok := queryer.(detailWithOptionsQueryer)
	if !ok {
		result, err := queryer.StudyDetail(c.Request.Context(), id)
		writeMLWHResult(c, result, err)

		return
	}

	result, total, err := withOptions.studyDetailWithOptions(c.Request.Context(), id, opts)
	writeMLWHDetailResult(c, result, err, opts, total, studyDetailReturned(result))
}

// writeMLWHDetailResult writes a detail response: on error it writes the error
// envelope and sets no headers, and on success it sets the X-Total-Count /
// X-Next-Offset headers (sizing the paginated nested collection by its full
// count) before writing the body. A lean response is the flat id lists, so it
// reports the full count with X-Next-Offset -1 (it is not itself paged).
func writeMLWHDetailResult(c *gin.Context, result any, err error, opts detailOptions, total, returned int) {
	if err != nil {
		writeMLWHError(c, err)

		return
	}

	if opts.lean {
		writeListSizingHeaders(c, total, total, 0)
	} else {
		writeListSizingHeaders(c, total, opts.offset, returned)
	}

	c.JSON(http.StatusOK, result)
}

// studyDetailReturned is the number of nested sample rows a study detail carries
// across its libraries, i.e. the length of its paginated nested collection.
func studyDetailReturned(detail StudyDetail) int {
	returned := 0
	for _, library := range detail.Libraries {
		returned += len(library.Samples)
	}

	return returned
}

// writeMLWHRunDetail serves a run detail with the same sizing-header behaviour as
// writeMLWHStudyDetail, sizing on the run's full nested sample count.
func writeMLWHRunDetail(c *gin.Context, queryer Queryer, id string, opts detailOptions) {
	withOptions, ok := queryer.(detailWithOptionsQueryer)
	if !ok {
		result, err := queryer.RunDetail(c.Request.Context(), id)
		writeMLWHResult(c, result, err)

		return
	}

	result, total, err := withOptions.runDetailWithOptions(c.Request.Context(), id, opts)
	writeMLWHDetailResult(c, result, err, opts, total, len(result.Samples))
}

func mlwhLibraryStudy(c *gin.Context) (string, string, bool) {
	pipeline, ok := mlwhPathParam(c, "pipeline")
	if !ok {
		return "", "", false
	}

	study, ok := mlwhPathParam(c, "study")
	if !ok {
		return "", "", false
	}

	return pipeline, study, true
}

// countSamplesWithDataResult dispatches the shared /study/:id/samples-with-data/count
// endpoint: with no since it returns the all-time CountSamplesWithData, and with
// a since (validated as RFC3339 by the handler) it returns the windowed
// CountSamplesWithDataSince when the queryer supports it, falling back to the
// all-time count otherwise. The since/until bounds have already been validated,
// so this never produces the 400 path.
func countSamplesWithDataResult(c *gin.Context, queryer Queryer, id, since, until string) (Count, error) {
	if since == "" {
		return queryer.CountSamplesWithData(c.Request.Context(), id)
	}

	if windowed, ok := queryer.(samplesWithDataSinceQueryer); ok {
		return windowed.CountSamplesWithDataSince(c.Request.Context(), id, since, until)
	}

	return queryer.CountSamplesWithData(c.Request.Context(), id)
}

// samplesWithDataResult dispatches the shared /study/:id/samples-with-data list
// endpoint: with no since it returns the all-time SamplesWithData, and with a
// since (validated as RFC3339 by the handler) it returns the windowed
// SamplesWithDataSince when the queryer supports it, falling back to the all-time
// list otherwise. The since/until bounds have already been validated, so this
// never produces the 400 path. Pagination is applied identically on both paths.
func samplesWithDataResult(c *gin.Context, queryer Queryer, id, since, until string, pagination mlwhPagination) ([]SampleWithData, error) {
	if since == "" {
		return queryer.SamplesWithData(c.Request.Context(), id, pagination.limit, pagination.offset)
	}

	if windowed, ok := queryer.(samplesWithDataSinceListQueryer); ok {
		return windowed.SamplesWithDataSince(c.Request.Context(), id, since, until, pagination.limit, pagination.offset)
	}

	return queryer.SamplesWithData(c.Request.Context(), id, pagination.limit, pagination.offset)
}

// mlwhDetailIDAndOptions reads the :id path param and the detail endpoints'
// optional limit/offset/lean query params (the nested collection's pagination and
// the lean flat-id-list switch). A malformed value aborts with the bad_request 400
// envelope before the queryer is reached.
func mlwhDetailIDAndOptions(c *gin.Context) (string, detailOptions, bool) {
	id, ok := mlwhPathParam(c, "id")
	if !ok {
		return "", detailOptions{}, false
	}

	pagination, ok := mlwhPaginationFromQuery(c)
	if !ok {
		return "", detailOptions{}, false
	}

	lean, ok := mlwhQueryBool(c, "lean")
	if !ok {
		return "", detailOptions{}, false
	}

	return id, detailOptions{limit: pagination.limit, offset: pagination.offset, lean: lean}, true
}

// mlwhQueryBool reads an optional boolean query param. An absent param is false; a
// present param accepts the usual strconv.ParseBool spellings (true/false/1/0/
// t/f). A malformed value aborts with the bad_request 400 envelope and reports
// false, so the handler returns before reaching the queryer.
func mlwhQueryBool(c *gin.Context, name string) (bool, bool) {
	raw := c.Query(name)
	if raw == "" {
		return false, true
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		writeMLWHBadRequest(c, "invalid "+name+": must be a boolean")

		return false, false
	}

	return value, true
}

// studyManifestCounter is the optional Queryer capability that sizes the manifest
// list by its product count (the C2 grain). The Client satisfies it; a queryer
// that does not falls back to no sizing headers, so the manifest endpoint works
// regardless. C2 (the public CountStudyManifest) will count over the same grain,
// so the standalone count and this sizing total never drift.
type studyManifestCounter interface {
	countStudyManifestProducts(ctx context.Context, studyLimsID string) (int, error)
}

// detailWithOptionsQueryer is the optional Queryer capability that builds a
// paginated / lean detail aggregate and reports the full nested-collection total
// for the X-Total-Count header. The Client satisfies it; a queryer that does not
// (e.g. a test fake) falls back to the plain detail method with no sizing headers,
// so the detail endpoints work regardless.
type detailWithOptionsQueryer interface {
	studyDetailWithOptions(ctx context.Context, studyLimsID string, opts detailOptions) (StudyDetail, int, error)
	runDetailWithOptions(ctx context.Context, idRun string, opts detailOptions) (RunDetail, int, error)
}

// samplesWithDataSinceQueryer is the windowed-count capability the
// /study/:id/samples-with-data/count handler needs when a since is supplied. It
// is satisfied by *Client and *RemoteClient. It is a narrow capability interface
// rather than a Queryer member because the windowed count shares the all-time
// count's single Registry endpoint (parameterised by the since/until query
// params), so it is not a distinct endpoint on the query surface.
type samplesWithDataSinceQueryer interface {
	CountSamplesWithDataSince(ctx context.Context, studyLimsID, since, until string) (Count, error)
}

// samplesWithDataSinceListQueryer is the windowed-list capability the
// /study/:id/samples-with-data handler needs when a since is supplied. It is
// satisfied by *Client and *RemoteClient. Like samplesWithDataSinceQueryer, it is
// a narrow capability interface rather than a Queryer member because the windowed
// list shares the all-time list's single Registry endpoint (parameterised by the
// since/until query params), so it is not a distinct endpoint on the query
// surface (preserving the 1:1 Method<->Registry invariant).
type samplesWithDataSinceListQueryer interface {
	SamplesWithDataSince(ctx context.Context, studyLimsID, since, until string, limit, offset int) ([]SampleWithData, error)
}

// mlwhHealthHandler answers GET /health with a static {"status":"ok"} body. It
// does not consult the queryer, so it succeeds regardless of sync state.
func mlwhHealthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// mlwhOpenAPIHandler answers GET /openapi.json with the generated OpenAPI 3.1.0
// document. Like /health it is a static plain route that does not consult the
// queryer, so it stays inexpensive and reachable without authentication. The
// document is built once and reused, so the reflection and large-map allocation
// behind it do not repeat per request.
func mlwhOpenAPIHandler(c *gin.Context) {
	c.JSON(http.StatusOK, memoizedOpenAPIDocument())
}

func mlwhQueryInt(c *gin.Context, name string, defaultValue int) (int, bool) {
	raw := c.Query(name)
	if raw == "" {
		return defaultValue, true
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		writeMLWHBadRequest(c, "invalid "+name)

		return 0, false
	}

	return value, true
}
