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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"github.com/wtsi-hgi/wa/saga"
	"github.com/wtsi-hgi/wa/seqmeta"
)

var listenFunc = net.Listen

type rootOptions struct {
	token   string
	baseURL string
	dbPath  string
}

func main() {
	cmd := newRootCommand()
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	options := &rootOptions{}

	command := &cobra.Command{
		Use:   "seqmeta",
		Short: "Sequence metadata cache CLI",
	}

	command.PersistentFlags().StringVar(&options.token, "token", os.Getenv("SAGA_API_TOKEN"), "SAGA API token")
	command.PersistentFlags().StringVar(&options.baseURL, "base-url", "", "SAGA base URL")
	command.PersistentFlags().StringVar(&options.dbPath, "db", "seqmeta.db", "SQLite database path")

	command.AddCommand(newDiffCommand(options))
	command.AddCommand(newValidateCommand(options))
	command.AddCommand(newServeCommand(options))

	return command
}

func newDiffCommand(options *rootOptions) *cobra.Command {
	var studyID string
	var sampleID string

	command := &cobra.Command{
		Use:   "diff",
		Short: "Diff study samples or sample files",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if (studyID == "" && sampleID == "") || (studyID != "" && sampleID != "") {
				return errors.New("usage: specify exactly one of --study or --sample")
			}

			provider, closeProvider, err := openProvider(options)
			if err != nil {
				return err
			}
			defer closeProvider()

			store, err := seqmeta.OpenStore(options.dbPath)
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			if studyID != "" {
				samples, err := provider.AllSamplesForStudy(ctx, studyID)
				if err != nil {
					return err
				}

				return store.WithLock(func() error {
					prepared, err := seqmeta.PrepareDiff(store, "study_samples:"+studyID, samples, func(sample saga.MLWHSample) string {
						return sample.SangerID
					})
					if err != nil {
						return err
					}

					body, err := marshalCommandJSON(prepared.Result)
					if err != nil {
						return err
					}

					if err := prepared.Commit(); err != nil {
						return err
					}

					if err := writeCommandJSON(cmd.OutOrStdout(), body); err != nil {
						return rollbackPreparedDiff(prepared, err)
					}

					return nil
				})
			}

			files, err := provider.GetSampleFiles(ctx, sampleID)
			if err != nil {
				return err
			}

			return store.WithLock(func() error {
				prepared, err := seqmeta.PrepareDiffSampleFiles(ctx, &prefetchedProvider{files: files}, store, sampleID)
				if err != nil {
					return err
				}

				body, err := marshalCommandJSON(prepared.Result)
				if err != nil {
					return err
				}

				if err := prepared.Commit(); err != nil {
					return err
				}

				if err := writeCommandJSON(cmd.OutOrStdout(), body); err != nil {
					return rollbackPreparedDiff(prepared, err)
				}

				return nil
			})
		},
	}

	command.Flags().StringVar(&studyID, "study", "", "Study ID")
	command.Flags().StringVar(&sampleID, "sample", "", "Sanger sample ID")

	return command
}

type prefetchedProvider struct {
	files []saga.IRODSFile
}

func (p *prefetchedProvider) GetStudy(context.Context, string) (*saga.Study, error) {
	return nil, errors.New("unused prefetched provider method")
}

func (p *prefetchedProvider) AllStudies(context.Context) ([]saga.Study, error) {
	return nil, errors.New("unused prefetched provider method")
}

func (p *prefetchedProvider) AllSamples(context.Context) ([]saga.MLWHSample, error) {
	return nil, errors.New("unused prefetched provider method")
}

func (p *prefetchedProvider) AllSamplesForStudy(context.Context, string) ([]saga.MLWHSample, error) {
	return nil, errors.New("unused prefetched provider method")
}

func (p *prefetchedProvider) GetSampleFiles(context.Context, string) ([]saga.IRODSFile, error) {
	return p.files, nil
}

func (p *prefetchedProvider) ListProjects(context.Context) ([]saga.Project, error) {
	return nil, errors.New("unused prefetched provider method")
}

func newValidateCommand(options *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "validate <identifier>",
		Short: "Validate and classify one identifier",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("usage: validate <identifier>")
			}

			provider, closeProvider, err := openProvider(options)
			if err != nil {
				return err
			}
			defer closeProvider()

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			result, err := seqmeta.Validate(ctx, provider, args[0])
			if err != nil {
				return err
			}

			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
}

func newServeCommand(options *rootOptions) *cobra.Command {
	var port int

	command := &cobra.Command{
		Use:   "serve",
		Short: "Serve the seqmeta HTTP API",
		RunE: func(cmd *cobra.Command, _ []string) error {
			provider, closeProvider, err := openProvider(options)
			if err != nil {
				return err
			}
			defer closeProvider()

			store, err := seqmeta.OpenStore(options.dbPath)
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()

			listener, err := listenFunc("tcp", fmt.Sprintf(":%d", port))
			if err != nil {
				return err
			}
			defer func() { _ = listener.Close() }()

			httpServer := &http.Server{Handler: seqmeta.NewServer(provider, store).Handler()}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			go func() {
				<-ctx.Done()
				_ = httpServer.Shutdown(context.Background())
			}()

			err = httpServer.Serve(listener)
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}

			return err
		},
	}

	command.Flags().IntVar(&port, "port", 8080, "Port to bind")

	return command
}

func openProvider(options *rootOptions) (seqmeta.SAGAProvider, func(), error) {
	clientOptions := []saga.Option{}
	if options.baseURL != "" {
		clientOptions = append(clientOptions, saga.WithBaseURL(options.baseURL))
	}

	client, err := saga.NewClient(options.token, clientOptions...)
	if err != nil {
		return nil, func() {}, err
	}

	return seqmeta.NewClientAdapter(client), func() { client.Close() }, nil
}

func marshalCommandJSON(payload any) ([]byte, error) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return nil, err
	}

	return body.Bytes(), nil
}

func writeCommandJSON(output io.Writer, body []byte) error {
	_, err := output.Write(body)

	return err
}

func rollbackPreparedDiff[T any](prepared *seqmeta.PreparedDiff[T], writeErr error) error {
	if rollbackErr := prepared.Rollback(); rollbackErr != nil {
		return errors.Join(writeErr, rollbackErr)
	}

	return writeErr
}
