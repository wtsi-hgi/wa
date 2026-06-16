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

// RegisterRoutes registers MLWH API routes on the provided Gin routers.
func (s *Server) RegisterRoutes(router *gin.Engine, auth *gin.RouterGroup) {
	if s == nil {
		return
	}

	if router != nil {
		configureMLWHRouter(router)
		registerMLWHEndpoints(router, s.queryer)
	}

	if auth != nil {
		registerMLWHEndpoints(auth, s.queryer)
	}
}

func configureMLWHRouter(router *gin.Engine) {
	router.UseRawPath = true
	router.UnescapePathValues = false
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
