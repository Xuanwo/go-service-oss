package oss

import (
	"fmt"
	"strings"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"

	ps "github.com/beyondstorage/go-storage/v4/pairs"
	"github.com/beyondstorage/go-storage/v4/pkg/credential"
	"github.com/beyondstorage/go-storage/v4/pkg/endpoint"
	"github.com/beyondstorage/go-storage/v4/pkg/httpclient"
	"github.com/beyondstorage/go-storage/v4/services"
	typ "github.com/beyondstorage/go-storage/v4/types"
)

// Service is the aliyun oss *Service config.
type Service struct {
	service *oss.Client

	defaultPairs DefaultServicePairs
	features     ServiceFeatures

	typ.UnimplementedServicer
}

// String implements Servicer.String
func (s *Service) String() string {
	return fmt.Sprintf("Servicer oss")
}

// Storage is the aliyun object storage service.
type Storage struct {
	bucket *oss.Bucket

	name    string
	workDir string

	defaultPairs DefaultStoragePairs
	features     StorageFeatures

	typ.UnimplementedStorager
	typ.UnimplementedAppender
	typ.UnimplementedMultiparter
	typ.UnimplementedDirer
}

// String implements Storager.String
func (s *Storage) String() string {
	return fmt.Sprintf(
		"Storager oss {Name: %s, WorkDir: %s}",
		s.bucket.BucketName, s.workDir,
	)
}

// New will create both Servicer and Storager.
func New(pairs ...typ.Pair) (typ.Servicer, typ.Storager, error) {
	return newServicerAndStorager(pairs...)
}

// NewServicer will create Servicer only.
func NewServicer(pairs ...typ.Pair) (typ.Servicer, error) {
	return newServicer(pairs...)
}

// NewStorager will create Storager only.
func NewStorager(pairs ...typ.Pair) (typ.Storager, error) {
	_, store, err := newServicerAndStorager(pairs...)
	return store, err
}

func newServicer(pairs ...typ.Pair) (srv *Service, err error) {
	defer func() {
		if err != nil {
			err = services.InitError{Op: "new_servicer", Type: Type, Err: formatError(err), Pairs: pairs}
		}
	}()

	srv = &Service{}

	opt, err := parsePairServiceNew(pairs)
	if err != nil {
		return nil, err
	}

	cp, err := credential.Parse(opt.Credential)
	if err != nil {
		return nil, err
	}
	if cp.Protocol() != credential.ProtocolHmac {
		return nil, services.PairUnsupportedError{Pair: ps.WithCredential(opt.Credential)}
	}
	ak, sk := cp.Hmac()

	ep, err := endpoint.Parse(opt.Endpoint)
	if err != nil {
		return nil, err
	}

	var copts []oss.ClientOption
	if opt.HasHTTPClientOptions {
		copts = append(copts, oss.HTTPClient(httpclient.New(opt.HTTPClientOptions)))
	}

	srv.service, err = oss.New(ep.String(), ak, sk, copts...)
	if err != nil {
		return nil, err
	}

	if opt.HasDefaultServicePairs {
		srv.defaultPairs = opt.DefaultServicePairs
	}
	if opt.HasServiceFeatures {
		srv.features = opt.ServiceFeatures
	}
	return
}
func newServicerAndStorager(pairs ...typ.Pair) (srv *Service, store *Storage, err error) {
	srv, err = newServicer(pairs...)
	if err != nil {
		return
	}

	store, err = srv.newStorage(pairs...)
	if err != nil {
		err = services.InitError{Op: "new_storager", Type: Type, Err: formatError(err), Pairs: pairs}
		return nil, nil, err
	}
	return srv, store, nil
}

// All available storage classes are listed here.
const (
	// ref: https://www.alibabacloud.com/help/doc-detail/31984.htm
	storageClassHeader = "x-oss-storage-class"

	// ref: https://www.alibabacloud.com/help/doc-detail/51374.htm
	StorageClassStandard = "STANDARD"
	StorageClassIA       = "IA"
	StorageClassArchive  = "Archive"
)

func formatError(err error) error {
	if _, ok := err.(services.InternalError); ok {
		return err
	}

	switch e := err.(type) {
	case oss.ServiceError:
		switch e.Code {
		case "":
			switch e.StatusCode {
			case 404:
				return fmt.Errorf("%w: %v", services.ErrObjectNotExist, err)
			default:
				return fmt.Errorf("%w, %v", services.ErrUnexpected, err)
			}
		case "NoSuchKey":
			return fmt.Errorf("%w: %v", services.ErrObjectNotExist, err)
		case "AccessDenied":
			return fmt.Errorf("%w: %v", services.ErrPermissionDenied, err)
		}
	case oss.UnexpectedStatusCodeError:
		switch e.Got() {
		case 404:
			return fmt.Errorf("%w: %v", services.ErrObjectNotExist, err)
		case 403:
			return fmt.Errorf("%w: %v", services.ErrPermissionDenied, err)
		}
	}

	return fmt.Errorf("%w, %v", services.ErrUnexpected, err)
}

// newStorage will create a new client.
func (s *Service) newStorage(pairs ...typ.Pair) (st *Storage, err error) {
	opt, err := parsePairStorageNew(pairs)
	if err != nil {
		return nil, err
	}

	bucket, err := s.service.Bucket(opt.Name)
	if err != nil {
		return nil, err
	}

	store := &Storage{
		bucket: bucket,

		workDir: "/",
	}

	if opt.HasDefaultStoragePairs {
		store.defaultPairs = opt.DefaultStoragePairs
	}
	if opt.HasStorageFeatures {
		store.features = opt.StorageFeatures
	}
	if opt.HasWorkDir {
		store.workDir = opt.WorkDir
	}
	return store, nil
}

func (s *Service) formatError(op string, err error, name string) error {
	if err == nil {
		return nil
	}

	return services.ServiceError{
		Op:       op,
		Err:      formatError(err),
		Servicer: s,
		Name:     name,
	}
}

// getAbsPath will calculate object storage's abs path
func (s *Storage) getAbsPath(path string) string {
	prefix := strings.TrimPrefix(s.workDir, "/")
	return prefix + path
}

// getRelPath will get object storage's rel path.
func (s *Storage) getRelPath(path string) string {
	prefix := strings.TrimPrefix(s.workDir, "/")
	return strings.TrimPrefix(path, prefix)
}

func (s *Storage) formatError(op string, err error, path ...string) error {
	if err == nil {
		return nil
	}

	return services.StorageError{
		Op:       op,
		Err:      formatError(err),
		Storager: s,
		Path:     path,
	}
}

func (s *Storage) formatFileObject(v oss.ObjectProperties) (o *typ.Object, err error) {
	o = s.newObject(false)
	o.ID = v.Key
	o.Path = s.getRelPath(v.Key)
	o.Mode |= typ.ModeRead

	o.SetContentLength(v.Size)
	o.SetLastModified(v.LastModified)

	if v.Type != "" {
		o.SetContentType(v.Type)
	}

	// OSS advise us don't use Etag as Content-MD5.
	//
	// ref: https://help.aliyun.com/document_detail/31965.html
	if v.ETag != "" {
		o.SetEtag(v.ETag)
	}

	var sm ObjectMetadata
	if value := v.Type; value != "" {
		sm.StorageClass = value
	}
	o.SetServiceMetadata(sm)

	return
}

func (s *Storage) newObject(done bool) *typ.Object {
	return typ.NewObject(s, done)
}

// All available encryption algorithms are listed here.
const (
	serverSideEncryptionHeader      = "x-oss-server-side-encryption"
	serverSideEncryptionKeyIdHeader = "x-oss-server-side-encryption-key-id"

	ServerSideEncryptionAES256 = "AES256"
	ServerSideEncryptionKMS    = "KMS"
	ServerSideEncryptionSM4    = "SM4"

	ServerSideDataEncryptionSM4 = "SM4"
)

// OSS response error code.
//
// ref: https://error-center.alibabacloud.com/status/product/Oss
const (
	// responseCodeNoSuchUpload will be returned while the specified upload does not exist.
	responseCodeNoSuchUpload = "NoSuchUpload"
)

func checkError(err error, code string) bool {
	e, ok := err.(oss.ServiceError)
	if !ok {
		return false
	}

	return e.Code == code
}

// multipartXXX are multipart upload restriction in OSS, see more details at:
// https://help.aliyun.com/document_detail/31993.html
const (
	// multipartNumberMaximum is the max part count supported.
	multipartNumberMaximum = 10000
	// multipartSizeMaximum is the maximum size for each part, 5GB.
	multipartSizeMaximum = 5 * 1024 * 1024 * 1024
	// multipartSizeMinimum is the minimum size for each part, 100KB.
	multipartSizeMinimum = 100 * 1024
)
