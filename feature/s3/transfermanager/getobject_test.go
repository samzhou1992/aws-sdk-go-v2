package transfermanager

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3testing "github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager/internal/testing"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager/types"
	"github.com/aws/aws-sdk-go-v2/internal/awstesting"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestGetObject(t *testing.T) {
	cases := map[string]struct {
		data              []byte
		errReaders        []s3testing.TestErrReader
		getObjectFn       func(*s3testing.TransferManagerLoggingClient, *s3.GetObjectInput) (*s3.GetObjectOutput, error)
		options           Options
		downloadRange     string
		expectInvocations int
		expectRanges      []string
		partNumber        int32
		partsCount        int32
		expectParts       []int32
		expectErr         string
		dataValidationFn  func(*testing.T, []byte)
	}{
		"range download in order": {
			data:        buf20MB,
			getObjectFn: s3testing.RangeGetObjectFn,
			options: Options{
				GetObjectType: types.GetObjectRanges,
				Concurrency:   1,
			},
			expectInvocations: 3,
			expectRanges:      []string{"bytes=0-8388607", "bytes=8388608-16777215", "bytes=16777216-20971519"},
		},
		"range download zero": {
			data:        []byte{},
			getObjectFn: s3testing.NonRangeGetObjectFn,
			options: Options{
				GetObjectType: types.GetObjectRanges,
			},
			expectInvocations: 1,
		},
		"range download with customized part size": {
			data:        buf20MB,
			getObjectFn: s3testing.RangeGetObjectFn,
			options: Options{
				GetObjectType: types.GetObjectRanges,
				PartSizeBytes: 10 * 1024 * 1024,
				Concurrency:   1,
			},
			expectInvocations: 2,
			expectRanges:      []string{"bytes=0-10485759", "bytes=10485760-20971519"},
		},
		"range download with s3 error": {
			data:        buf20MB,
			getObjectFn: s3testing.ErrRangeGetObjectFn,
			options: Options{
				GetObjectType: types.GetObjectRanges,
			},
			expectInvocations: 2,
			expectErr:         "s3 service error",
		},
		"content length download single chunk": {
			data:        buf2MB,
			getObjectFn: s3testing.NonRangeGetObjectFn,
			options: Options{
				GetObjectType: types.GetObjectRanges,
			},
			expectInvocations: 1,
			expectRanges:      []string{"bytes=0-2097151"},
			dataValidationFn: func(t *testing.T, bytes []byte) {
				count := 0
				for _, b := range bytes {
					count += int(b)
				}
				if count != 0 {
					t.Errorf("expect 0 count, got %d", count)
				}
			},
		},
		"range download single chunk": {
			data:        buf2MB,
			getObjectFn: s3testing.RangeGetObjectFn,
			options: Options{
				GetObjectType: types.GetObjectRanges,
			},
			expectInvocations: 1,
			expectRanges:      []string{"bytes=0-2097151"},
			dataValidationFn: func(t *testing.T, bytes []byte) {
				count := 0
				for _, b := range bytes {
					count += int(b)
				}
				if count != 0 {
					t.Errorf("expect 0 count, got %d", count)
				}
			},
		},
		"range download success without retry": {
			data:        []byte("123"),
			getObjectFn: s3testing.ErrReaderFn,
			errReaders: []s3testing.TestErrReader{
				{Buf: []byte("123"), Len: 3, Err: io.EOF},
			},
			options: Options{
				GetObjectType: types.GetObjectRanges,
			},
			expectInvocations: 1,
			dataValidationFn: func(t *testing.T, bytes []byte) {
				if e, a := "123", string(bytes); e != a {
					t.Errorf("expect %q response, got %q", e, a)
				}
			},
		},
		"range download fail retry": {
			data:        []byte("ab"),
			getObjectFn: s3testing.ErrReaderFn,
			errReaders: []s3testing.TestErrReader{
				{Buf: []byte("ab"), Len: 2, Err: io.ErrUnexpectedEOF},
			},
			options: Options{
				GetObjectType: types.GetObjectRanges,
			},
			expectInvocations: 1,
			expectErr:         "unexpected EOF",
		},
		"range download a range of object": {
			data:        buf20MB,
			getObjectFn: s3testing.RangeGetObjectFn,
			options: Options{
				GetObjectType: types.GetObjectRanges,
				Concurrency:   1,
			},
			downloadRange:     "bytes=0-10485759",
			expectInvocations: 2,
			expectRanges:      []string{"bytes=0-8388607", "bytes=8388608-10485759"},
		},
		"parts download in order": {
			data:        buf2MB,
			getObjectFn: s3testing.PartGetObjectFn,
			options: Options{
				Concurrency: 1,
			},
			partsCount:        3,
			expectInvocations: 3,
			expectParts:       []int32{1, 2, 3},
		},
		"part download zero": {
			data:              buf2MB,
			getObjectFn:       s3testing.PartGetObjectFn,
			options:           Options{},
			partsCount:        1,
			expectInvocations: 1,
			expectParts:       []int32{1},
		},
		"part download with s3 error": {
			data:              buf2MB,
			getObjectFn:       s3testing.ErrPartGetObjectFn,
			options:           Options{},
			partsCount:        3,
			expectInvocations: 2,
			expectErr:         "s3 service error",
		},
		"part download single chunk": {
			data:              []byte("123"),
			getObjectFn:       s3testing.PartGetObjectFn,
			options:           Options{},
			partsCount:        1,
			expectInvocations: 1,
			expectParts:       []int32{1},
			dataValidationFn: func(t *testing.T, bytes []byte) {
				if e, a := "123", string(bytes); e != a {
					t.Errorf("expect %q response, got %q", e, a)
				}
			},
		},
		"part download success without retry": {
			getObjectFn: s3testing.ErrReaderFn,
			errReaders: []s3testing.TestErrReader{
				{Buf: []byte("ab"), Len: 2, Err: io.EOF},
			},
			options:           Options{},
			partsCount:        1,
			expectInvocations: 1,
			expectParts:       []int32{1},
			dataValidationFn: func(t *testing.T, bytes []byte) {
				if e, a := "ab", string(bytes); e != a {
					t.Errorf("expect %q response, got %q", e, a)
				}
			},
		},
		"part download fail retry": {
			data:        []byte("ab"),
			getObjectFn: s3testing.ErrReaderFn,
			errReaders: []s3testing.TestErrReader{
				{Buf: []byte("ab"), Len: 2, Err: io.ErrUnexpectedEOF},
			},
			options:           Options{},
			expectInvocations: 1,
			expectErr:         "unexpected EOF",
			dataValidationFn: func(t *testing.T, bytes []byte) {
				if e, a := "ab", string(bytes); e != a {
					t.Errorf("expect %q response, got %q", e, a)
				}
			},
		},
		"parts download with range input": {
			data:              []byte("123"),
			getObjectFn:       s3testing.PartGetObjectFn,
			options:           Options{},
			downloadRange:     "bytes=0-100",
			partsCount:        3,
			expectInvocations: 1,
			dataValidationFn: func(t *testing.T, bytes []byte) {
				if e, a := "123", string(bytes); e != a {
					t.Errorf("expect %q response, got %q", e, a)
				}
			},
		},
		"parts download with part number input": {
			data:              []byte("ab"),
			getObjectFn:       s3testing.PartGetObjectFn,
			options:           Options{},
			partsCount:        3,
			partNumber:        5,
			expectInvocations: 1,
			dataValidationFn: func(t *testing.T, bytes []byte) {
				if e, a := "ab", string(bytes); e != a {
					t.Errorf("expect %q response, got %q", e, a)
				}
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s3Client, invocations, parts, ranges := s3testing.NewDownloadClient()
			s3Client.Data = c.data
			s3Client.GetObjectFn = c.getObjectFn
			s3Client.ErrReaders = c.errReaders
			s3Client.PartsCount = c.partsCount
			mgr := New(s3Client, c.options)

			input := &GetObjectInput{
				Bucket: "bucket",
				Key:    "key",
			}
			input.Range = c.downloadRange
			input.PartNumber = c.partNumber
			r := NewConcurrentReader()
			input.Reader = r

			var wg sync.WaitGroup
			actualBuf := make([]byte, 0)

			wg.Add(1)
			go func() {
				defer wg.Done()
				b, err := io.ReadAll(r)
				if err != nil {
					if c.expectErr == "" {
						t.Errorf("expect no error when copying file, got %q", err)
					} else if e, a := c.expectErr, err.Error(); !strings.Contains(a, e) {
						t.Errorf("expect %s error message to be in %s", e, a)
					}
					//return
				} else if c.expectErr != "" {
					t.Error("expect an error, but got none")
					//return
				}

				actualBuf = append(actualBuf, b...)
			}()

			_, err := mgr.GetObject(context.Background(), input)
			wg.Wait()

			if err != nil {
				if c.expectErr == "" {
					t.Fatalf("expect no error, got %q", err)
				} else if e, a := c.expectErr, err.Error(); !strings.Contains(a, e) {
					t.Fatalf("expect %s error message to be in %s", e, a)
				}
			} else if c.expectErr != "" {
				t.Fatal("expect error, got nil")
			}

			if err != nil {
				return
			}

			if e, a := c.expectInvocations, *invocations; e != a {
				t.Errorf("expect %v API calls, got %v", e, a)
			}

			if len(c.expectParts) > 0 {
				if e, a := c.expectParts, *parts; !reflect.DeepEqual(e, a) {
					t.Errorf("expect %v parts, got %v", e, a)
				}
			}
			if len(c.expectRanges) > 0 {
				if e, a := c.expectRanges, *ranges; !reflect.DeepEqual(e, a) {
					t.Errorf("expect %v ranges, got %v", e, a)
				}
			}

			if c.dataValidationFn != nil {
				c.dataValidationFn(t, actualBuf)
			}
		})
	}
}

func TestGetAsyncWithFailure(t *testing.T) {
	cases := map[string]struct {
		downloadType types.GetObjectType
	}{
		"part download by default": {},
		"range download": {
			downloadType: types.GetObjectRanges,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			startingByte := 0
			reqCount := int64(0)

			s3Client, _, _, _ := s3testing.NewDownloadClient()
			s3Client.PartsCount = 10
			s3Client.Data = buf40MB
			s3Client.GetObjectFn = func(c *s3testing.TransferManagerLoggingClient, params *s3.GetObjectInput) (out *s3.GetObjectOutput, err error) {
				switch atomic.LoadInt64(&reqCount) {
				case 1:
					// Give a chance for the multipart chunks to be queued up
					time.Sleep(1 * time.Second)
					err = fmt.Errorf("some connection error")
				default:
					body := bytes.NewReader(make([]byte, minPartSizeBytes))
					out = &s3.GetObjectOutput{
						Body:          ioutil.NopCloser(body),
						ContentLength: aws.Int64(int64(body.Len())),
						ContentRange:  aws.String(fmt.Sprintf("bytes %d-%d/%d", startingByte, body.Len()-1, body.Len()*10)),
					}

					startingByte += body.Len()
					if reqCount > 0 {
						// sleep here to ensure context switching between goroutines
						time.Sleep(25 * time.Millisecond)
					}
				}
				atomic.AddInt64(&reqCount, 1)
				return out, err
			}

			mgr := New(s3Client, Options{
				Concurrency:   2,
				GetObjectType: c.downloadType,
			})
			r := NewConcurrentReader()
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = io.ReadAll(r)
			}()

			// Expect this request to exit quickly after failure
			_, err := mgr.GetObject(context.Background(), &GetObjectInput{
				Bucket: "Bucket",
				Key:    "Key",
				Reader: r,
			})
			wg.Wait()

			if err == nil {
				t.Fatal("expect error, got none")
			} else if e, a := "some connection error", err.Error(); !strings.Contains(a, e) {
				t.Fatalf("expect %s error message to be in %s", e, a)
			}

			if atomic.LoadInt64(&reqCount) > 3 {
				t.Errorf("expect no more than 3 requests, but received %d", reqCount)
			}
		})
	}
}

func TestGetObjectWithContextCanceled(t *testing.T) {
	cases := map[string]struct {
		downloadType types.GetObjectType
	}{
		"part download by default": {},
		"range download": {
			downloadType: types.GetObjectRanges,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			mgr := New(s3.New(s3.Options{
				Region: "mock-region",
			}), Options{
				GetObjectType: c.downloadType,
			})

			ctx := &awstesting.FakeContext{DoneCh: make(chan struct{})}
			ctx.Error = fmt.Errorf("context canceled")
			close(ctx.DoneCh)

			r := NewConcurrentReader()
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = io.ReadAll(r)
			}()
			_, err := mgr.GetObject(ctx, &GetObjectInput{
				Bucket: "bucket",
				Key:    "Key",
				Reader: r,
			})
			wg.Wait()

			if err == nil {
				t.Fatalf("expected error, did not get one")
			}
			if e, a := "canceled", err.Error(); !strings.Contains(a, e) {
				t.Errorf("expected error message to contain %q, but did not %q", e, a)
			}
		})
	}
}
