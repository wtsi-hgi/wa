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
	"fmt"
	"net/http"
	"net/url"
	"strconv"

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

	offset, ok := mlwhQueryInt(c, "offset", 0)
	if !ok {
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
			result, err := queryer.AllStudies(c.Request.Context(), pagination.limit, pagination.offset)
			writeMLWHResult(c, result, err)
		}
	case "SamplesForStudy":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			result, err := queryer.SamplesForStudy(c.Request.Context(), id, pagination.limit, pagination.offset)
			writeMLWHResult(c, result, err)
		}
	case "SamplesForRun":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			result, err := queryer.SamplesForRun(c.Request.Context(), id, pagination.limit, pagination.offset)
			writeMLWHResult(c, result, err)
		}
	case "SamplesForLibrary":
		return func(c *gin.Context) {
			pipeline, study, pagination, ok := mlwhLibraryStudyAndPagination(c)
			if !ok {
				return
			}
			result, err := queryer.SamplesForLibrary(c.Request.Context(), pipeline, study, pagination.limit, pagination.offset)
			writeMLWHResult(c, result, err)
		}
	case "SamplesForLibraryID":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			result, err := queryer.SamplesForLibraryID(c.Request.Context(), id, pagination.limit, pagination.offset)
			writeMLWHResult(c, result, err)
		}
	case "SamplesForLibraryLimsID":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			result, err := queryer.SamplesForLibraryLimsID(c.Request.Context(), id, pagination.limit, pagination.offset)
			writeMLWHResult(c, result, err)
		}
	case "SamplesForLibraryType":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			result, err := queryer.SamplesForLibraryType(c.Request.Context(), id, pagination.limit, pagination.offset)
			writeMLWHResult(c, result, err)
		}
	case "LibrariesForStudy":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			result, err := queryer.LibrariesForStudy(c.Request.Context(), id, pagination.limit, pagination.offset)
			writeMLWHResult(c, result, err)
		}
	case "RunsForStudy":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			result, err := queryer.RunsForStudy(c.Request.Context(), id, pagination.limit, pagination.offset)
			writeMLWHResult(c, result, err)
		}
	case "LanesForSample":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			result, err := queryer.LanesForSample(c.Request.Context(), id, pagination.limit, pagination.offset)
			writeMLWHResult(c, result, err)
		}
	case "IRODSPathsForSample":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			result, err := queryer.IRODSPathsForSample(c.Request.Context(), id, pagination.limit, pagination.offset)
			writeMLWHResult(c, result, err)
		}
	case "IRODSPathsForStudy":
		return func(c *gin.Context) {
			id, pagination, ok := mlwhIDAndPagination(c)
			if !ok {
				return
			}
			result, err := queryer.IRODSPathsForStudy(c.Request.Context(), id, pagination.limit, pagination.offset)
			writeMLWHResult(c, result, err)
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
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.StudyDetail(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
		}
	case "RunDetail":
		return func(c *gin.Context) {
			id, ok := mlwhPathParam(c, "id")
			if !ok {
				return
			}
			result, err := queryer.RunDetail(c.Request.Context(), id)
			writeMLWHResult(c, result, err)
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
			result, err := queryer.SearchStudies(c.Request.Context(), term, pagination.limit, pagination.offset)
			writeMLWHResult(c, result, err)
		}
	case "SearchSamples":
		return func(c *gin.Context) {
			term, pagination, ok := mlwhTermAndSearchPagination(c)
			if !ok {
				return
			}
			result, err := queryer.SearchSamples(c.Request.Context(), term, pagination.limit, pagination.offset)
			writeMLWHResult(c, result, err)
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
