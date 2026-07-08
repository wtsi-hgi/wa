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

const mlwhQueryCommandConfigurationHelp = `Normal CLI users should point this command at the MLWH query server
with --server or WA_MLWH_SERVER_URL; database and cache credentials
stay with the server process. When WA_ENV selects a scenario and no
server URL is set, the command defaults to the active local MLWH API
port from WA_*_SEQMETA_PORT. Operators can still run against a local
cache with WA_MLWH_CACHE_PATH, or use WA_MLWH_DSN for direct local
operator mode.

Configuration is read from the environment. Use the persistent --env
flag (or WA_ENV=development|test|production) to load matching
.env.<name> / .env.<name>.local files from the working directory
before resolving:

  WA_MLWH_SERVER_URL      Preferred. Base URL for wa mlwh serve.
  WA_MLWH_BACKEND_URL     Lower-precedence compatibility default.
  WA_*_SEQMETA_PORT       Scenario-local default API port.
  WA_MLWH_DSN             Optional direct operator mode only.
  WA_MLWH_PASSWORD        Optional. Password used with WA_MLWH_DSN.
  WA_MLWH_CACHE_PATH      Optional local operator cache path or
                          MySQL cache DSN without a password.
  WA_MLWH_CACHE_PASSWORD  Optional. SQLCipher key used to encrypt
                          the local cache when set.`

const mlwhInfoCommandConfigurationHelp = mlwhQueryCommandConfigurationHelp + `

Local operator note: wa mlwh sync requires WA_MLWH_DSN when you are maintaining
the cache yourself.`
