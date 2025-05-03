package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinioStorage implements the ObjectStorageProvider interface using Minio.
type MinioStorage struct {
	client     *minio.Client
	core       *minio.Core // Needed for multipart uploads
	bucketName string
}

// Ensure MinioStorage implements ObjectStorageProvider (will fail compilation initially)
var _ ObjectStorageProvider = (*MinioStorage)(nil)

// Constants for storage provider types (used for config only)
type StorageProvider string

const (
	ProviderBackblaze StorageProvider = "backblaze"
	ProviderWasabi    StorageProvider = "wasabi"
	ProviderVultr     StorageProvider = "vultr"
	ProviderLocal     StorageProvider = "local"
	ProviderCluster   StorageProvider = "cluster"
)

type StorageConfig struct {
	Provider        StorageProvider
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	BucketName      string
	UseSSL          bool
	// Local storage specific
	LocalPath string
	// Cluster specific
	ClusterNodes []string
}

func getProviderEndpoint(provider StorageProvider, region string) string {
	switch provider {
	case ProviderWasabi:
		return fmt.Sprintf("s3.%s.wasabi.com", region)
	case ProviderVultr:
		return fmt.Sprintf("%s.vultrobjects.com", region)
	case ProviderLocal:
		return "localhost:9000" // MinIO server in filesystem mode
	case ProviderCluster:
		if nodeEndpoint := os.Getenv("MINIO_CLUSTER_NODES"); nodeEndpoint != "" {
			return nodeEndpoint // Use first node as primary endpoint
		}
		return "localhost:9000"
	default: // Backblaze
		return os.Getenv("S3_ENDPOINT")
	}
}

func InitMinio() error {
	provider := StorageProvider(os.Getenv("STORAGE_PROVIDER"))
	config := StorageConfig{
		Provider: provider,
		UseSSL:   true,
	}

	switch provider {
	case ProviderLocal:
		localPath := os.Getenv("LOCAL_STORAGE_PATH")
		if localPath == "" {
			return fmt.Errorf("LOCAL_STORAGE_PATH must be set for local storage provider")
		}
		// Ensure directory exists
		if err := os.MkdirAll(localPath, 0750); err != nil {
			return fmt.Errorf("failed to create local storage directory: %w", err)
		}
		config.LocalPath = localPath
		config.AccessKeyID = os.Getenv("MINIO_ROOT_USER")
		if config.AccessKeyID == "" {
			config.AccessKeyID = "minioadmin" // Default MinIO credentials
		}
		config.SecretAccessKey = os.Getenv("MINIO_ROOT_PASSWORD")
		if config.SecretAccessKey == "" {
			config.SecretAccessKey = "minioadmin"
		}
		config.BucketName = "arkfile"
		config.UseSSL = false // Local filesystem mode doesn't use SSL

	case ProviderCluster:
		nodeEndpoint := os.Getenv("MINIO_CLUSTER_NODES")
		if nodeEndpoint == "" {
			return fmt.Errorf("MINIO_CLUSTER_NODES must be set for cluster provider")
		}
		config.ClusterNodes = []string{nodeEndpoint} // For now, just use first node
		config.AccessKeyID = os.Getenv("MINIO_CLUSTER_ACCESS_KEY")
		config.SecretAccessKey = os.Getenv("MINIO_CLUSTER_SECRET_KEY")
		if config.AccessKeyID == "" || config.SecretAccessKey == "" {
			return fmt.Errorf("MINIO_CLUSTER_ACCESS_KEY and MINIO_CLUSTER_SECRET_KEY must be set")
		}
		config.BucketName = os.Getenv("MINIO_CLUSTER_BUCKET")
		if config.BucketName == "" {
			config.BucketName = "arkfile"
		}

	default: // External providers (Backblaze, Wasabi, Vultr)
		config.Region = os.Getenv("S3_REGION")
		config.AccessKeyID = os.Getenv("S3_ACCESS_KEY_ID")
		config.SecretAccessKey = os.Getenv("S3_SECRET_KEY")
		config.BucketName = os.Getenv("S3_BUCKET_NAME")
		// Validate region for providers that require it
		if (provider == ProviderWasabi || provider == ProviderVultr) && config.Region == "" {
			return fmt.Errorf("region is required for %s provider", provider)
		}
		// Validate required fields
		if config.AccessKeyID == "" || config.SecretAccessKey == "" || config.BucketName == "" {
			return fmt.Errorf("missing required storage configuration for provider %s", provider)
		}
	}

	// Set endpoint based on provider
	config.Endpoint = getProviderEndpoint(provider, config.Region)
	if config.Endpoint == "" {
		return fmt.Errorf("failed to determine endpoint for provider %s", provider)
	}

	// Create the Minio client instance locally
	client, err := minio.New(config.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKeyID, config.SecretAccessKey, ""),
		Secure: config.UseSSL,
	})
	if err != nil {
		return fmt.Errorf("failed to create MinIO client: %w", err)
	}

	// Initialize core locally
	core := &minio.Core{Client: client}

	// Assign the concrete implementation to the global interface variable
	Provider = &MinioStorage{
		client:     client,
		core:       core,
		bucketName: config.BucketName, // Store bucket name in the struct
	}

	// Ensure bucket exists using the local client variable
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := client.BucketExists(ctx, config.BucketName)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}

	if !exists {
		// Pass local client explicitly
		err = createBucket(ctx, client, config.BucketName)
		if err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
	}

	// Set bucket policy for private access using local client
	err = setBucketPolicy(ctx, client, config.BucketName)
	if err != nil {
		return fmt.Errorf("failed to set bucket policy: %w", err)
	}

	return nil
}

// Update helper to accept client
func createBucket(ctx context.Context, client *minio.Client, bucketName string) error {
	err := client.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
	if err != nil {
		return fmt.Errorf("failed to create bucket: %w", err)
	}
	log.Printf("Created new bucket: %s", bucketName)
	return nil
}

// Update helper to accept client
func setBucketPolicy(ctx context.Context, client *minio.Client, bucketName string) error {
	// Set a private policy
	policy := `{
        "Version": "2012-10-17",
        "Statement": [
            {
                "Effect": "Deny",
                "Principal": "*",
                "Action": "s3:*",
                "Resource": [
                    "arn:aws:s3:::%s/*",
                    "arn:aws:s3:::%s"
                ]
            }
        ]
    }`
	policy = fmt.Sprintf(policy, bucketName, bucketName)

	// Use the passed client
	err := client.SetBucketPolicy(ctx, bucketName, policy)
	if err != nil {
		return fmt.Errorf("failed to set bucket policy: %w", err)
	}
	return nil
}

// --- ObjectStorageProvider Interface Implementation ---

func (m *MinioStorage) PutObject(ctx context.Context, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	// Re-add "io" import if needed
	return m.client.PutObject(ctx, m.bucketName, objectName, reader, objectSize, opts)
}

// GetObject retrieves an object satisfying the ReadableStoredObject interface.
func (m *MinioStorage) GetObject(ctx context.Context, objectName string, opts minio.GetObjectOptions) (ReadableStoredObject, error) {
	// *minio.Object implicitly satisfies ReadableStoredObject
	return m.client.GetObject(ctx, m.bucketName, objectName, opts)
}

func (m *MinioStorage) RemoveObject(ctx context.Context, objectName string, opts minio.RemoveObjectOptions) error {
	return m.client.RemoveObject(ctx, m.bucketName, objectName, opts)
}

// --- Other Helper Methods on MinioStorage ---

// GetPresignedURL generates a temporary URL for file download
func (m *MinioStorage) GetPresignedURL(ctx context.Context, objectName string, expiry time.Duration) (string, error) {
	// Use context passed in if appropriate, or background if not time-sensitive
	url, err := m.client.PresignedGetObject(ctx, m.bucketName, objectName, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}
	return url.String(), nil
}

// InitiateMultipartUpload starts a new multipart upload
func (m *MinioStorage) InitiateMultipartUpload(ctx context.Context, objectName string, metadata map[string]string) (string, error) {
	// Initialize multipart upload using the core client
	uploadOptions := minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	}

	// Add user metadata if provided
	if metadata != nil {
		uploadOptions.UserMetadata = metadata
	}

	uploadID, err := m.core.NewMultipartUpload(ctx, m.bucketName, objectName, uploadOptions)
	if err != nil {
		return "", fmt.Errorf("failed to initialize multipart upload: %w", err)
	}

	return uploadID, nil
}

// UploadPart uploads a single part of a multipart upload
func (m *MinioStorage) UploadPart(ctx context.Context, objectName, uploadID string, partNumber int, reader io.Reader, size int64) (minio.CompletePart, error) {
	// Upload the part using the core client
	part, err := m.core.PutObjectPart(ctx, m.bucketName, objectName, uploadID, partNumber, reader, size, minio.PutObjectPartOptions{})
	if err != nil {
		return minio.CompletePart{}, fmt.Errorf("failed to upload part %d: %w", partNumber, err)
	}

	return minio.CompletePart{
		PartNumber: partNumber,
		ETag:       part.ETag,
	}, nil
}

// CompleteMultipartUpload finalizes a multipart upload
func (m *MinioStorage) CompleteMultipartUpload(ctx context.Context, objectName, uploadID string, parts []minio.CompletePart) error {
	// Complete the multipart upload using the core client
	_, err := m.core.CompleteMultipartUpload(ctx, m.bucketName, objectName, uploadID, parts, minio.PutObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to complete multipart upload: %w", err)
	}

	return nil
}

// AbortMultipartUpload cancels a multipart upload
func (m *MinioStorage) AbortMultipartUpload(ctx context.Context, objectName, uploadID string) error {
	// Abort the multipart upload using the core client
	err := m.core.AbortMultipartUpload(ctx, m.bucketName, objectName, uploadID)
	if err != nil {
		return fmt.Errorf("failed to abort multipart upload: %w", err)
	}

	return nil
}

// RemoveChunkedFile properly cleans up a chunked file, including any associated multipart uploads.
func (m *MinioStorage) RemoveChunkedFile(ctx context.Context, filename string, sessionID string) error {
	// First, remove the completed file if it exists using the interface method
	// Use RemoveObjectOptions{} for default behavior
	err := m.RemoveObject(ctx, filename, minio.RemoveObjectOptions{})
	if err != nil {
		// Log error but continue - we still want to try removing chunks
		log.Printf("Warning: Failed to remove complete file %s during chunked removal: %v", filename, err)
	}

	// List any incomplete uploads for this file (in case upload was never completed)
	multipartUploads := m.client.ListIncompleteUploads(ctx, m.bucketName, filename, true)

	for upload := range multipartUploads {
		if upload.Err != nil {
			log.Printf("Warning: Error listing incomplete uploads for %s: %v", filename, upload.Err)
			continue // Skip this problematic listing
		}

		// If sessionID is provided, only remove the specific upload
		if sessionID != "" && upload.UploadID != sessionID {
			continue
		}

		// Abort the multipart upload to clean up all chunks using the method
		err := m.AbortMultipartUpload(ctx, filename, upload.UploadID)
		if err != nil {
			// Log error but continue, try to abort others if any
			log.Printf("Warning: Failed to abort multipart upload %s for %s: %v", upload.UploadID, filename, err)
		} else {
			log.Printf("Aborted incomplete multipart upload %s for %s", upload.UploadID, filename)
		}
	}

	// Errors from the ListIncompleteUploads channel are handled within the loop via upload.Err

	return nil // Return nil even if some aborts failed, as the main object removal was attempted.
}

// GetObjectChunk downloads a specific byte range of an object
func (m *MinioStorage) GetObjectChunk(ctx context.Context, objectName string, offset, length int64) (io.ReadCloser, error) {
	// Create opts with byte range
	opts := minio.GetObjectOptions{}
	err := opts.SetRange(offset, offset+length-1)
	if err != nil {
		return nil, fmt.Errorf("failed to set byte range: %w", err)
	}

	// Get the object with specified range using the interface method
	object, err := m.GetObject(ctx, objectName, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get object chunk: %w", err)
	}

	return object, nil
}
