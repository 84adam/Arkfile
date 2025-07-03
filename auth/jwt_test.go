package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/84adam/arkfile/config" // Import config
	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

// TestMain sets up necessary environment variables for config loading before running tests
// and cleans them up afterwards.
func TestMain(m *testing.M) {
	// --- Test Config Setup ---
	config.ResetConfigForTest()

	// Store original env vars and set test values
	originalEnv := map[string]string{}
	testEnv := map[string]string{
		"JWT_SECRET":          "test-jwt-secret-for-auth", // Unique secret for this package's tests
		"STORAGE_PROVIDER":    "local",                    // Set storage provider to local (supports MinIO)
		"MINIO_ROOT_USER":     "test-user-auth",           // Provide dummy values for all required fields
		"MINIO_ROOT_PASSWORD": "test-password-auth",
		"LOCAL_STORAGE_PATH":  "/tmp/test-storage-auth", // Required for local storage
	}

	for key, testValue := range testEnv {
		originalEnv[key] = os.Getenv(key)
		os.Setenv(key, testValue)
	}

	// Load config with test env vars
	_, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("FATAL: Failed to load config for auth tests: %v\n", err)
		os.Exit(1)
	}

	// Run tests
	exitCode := m.Run()

	// --- Cleanup ---
	for key, originalValue := range originalEnv {
		if originalValue == "" {
			os.Unsetenv(key)
		} else {
			os.Setenv(key, originalValue)
		}
	}
	config.ResetConfigForTest()

	os.Exit(exitCode)
}

func TestGenerateToken(t *testing.T) {
	// Config with JWT_SECRET is loaded in TestMain

	testCases := []struct {
		name  string
		email string
	}{
		{"Valid email", "user@example.com"},
		{"Admin email", "admin@example.com"},
		{"Empty email", ""}, // Test edge case
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Execute
			tokenString, err := GenerateToken(tc.email)

			// Assert: Check for errors and non-empty token
			assert.NoError(t, err)
			assert.NotEmpty(t, tokenString)

			// Assert: Verify token structure and claims
			token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
				// Validate the alg is what you expect:
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid // Use standard error
				}
				// Validate using the secret loaded from config by TestMain
				return []byte(config.GetConfig().Security.JWTSecret), nil
			})

			assert.NoError(t, err)
			assert.True(t, token.Valid, "Token should be valid")

			claims, ok := token.Claims.(*Claims)
			assert.True(t, ok, "Claims should be of type *Claims")
			assert.Equal(t, tc.email, claims.Email, "Email claim should match")
			assert.Equal(t, "arkfile-auth", claims.Issuer, "Issuer claim should be correct")
			assert.Contains(t, claims.Audience, "arkfile-api", "Audience claim should contain 'arkfile-api'")
			assert.NotEmpty(t, claims.ID, "ID (jti) claim should not be empty")

			// Assert: Verify expiry time is approximately correct
			expectedExpiry := time.Now().Add(24 * time.Hour)
			// Allow a small delta (e.g., 5 seconds) for timing differences
			assert.WithinDuration(t, expectedExpiry, claims.ExpiresAt.Time, 5*time.Second, "Expiry time should be around 24 hours")
			assert.True(t, claims.IssuedAt.Time.Before(time.Now().Add(time.Second)), "Issue time should be in the past")
			assert.True(t, claims.NotBefore.Time.Before(time.Now().Add(time.Second)), "NotBefore time should be in the past")
		})
	}
}

func TestGetEmailFromToken(t *testing.T) {
	// Setup: Create a valid token for testing
	testEmail := "test@example.com"
	claims := &Claims{
		Email: testEmail,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)), // Valid expiry
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	// Note: No need to sign it as we're manually setting it in the context

	// Setup: Create an Echo context
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Setup: Set the *jwt.Token in the context (emulates middleware)
	c.Set("user", token)

	// Execute
	extractedEmail := GetEmailFromToken(c)

	// Assert
	assert.Equal(t, testEmail, extractedEmail, "Extracted email should match the one in the token")
}

func TestJWTMiddleware(t *testing.T) {
	// Config with JWT_SECRET is loaded in TestMain

	// Setup: Create Echo instance and test handler
	e := echo.New()
	mockHandler := func(c echo.Context) error {
		// We can verify the claims are set correctly by the middleware
		user := c.Get("user").(*jwt.Token)
		claims := user.Claims.(*Claims)
		assert.Equal(t, "test@example.com", claims.Email)
		return c.String(http.StatusOK, "test passed")
	}

	// Setup: Get the middleware function
	middlewareFunc := JWTMiddleware()
	handlerWithMiddleware := middlewareFunc(mockHandler)

	// Test cases
	testCases := []struct {
		name           string
		tokenFunc      func() string // Function to generate token for the test
		expectedStatus int
		expectBody     string
		expectError    bool
	}{
		{
			name: "Valid Token",
			tokenFunc: func() string {
				claims := &Claims{
					Email: "test@example.com",
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
						ID:        "valid-token-id",
					},
				}
				token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				// Sign with the secret loaded from config by TestMain
				tokenString, _ := token.SignedString([]byte(config.GetConfig().Security.JWTSecret))
				return tokenString
			},
			expectedStatus: http.StatusOK,
			expectBody:     "test passed",
			expectError:    false,
		},
		{
			name: "Expired Token",
			tokenFunc: func() string {
				claims := &Claims{
					Email: "test@example.com",
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)), // Expired
						ID:        "expired-token-id",
					},
				}
				token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				// Sign with the secret loaded from config by TestMain
				tokenString, _ := token.SignedString([]byte(config.GetConfig().Security.JWTSecret))
				return tokenString
			},
			expectedStatus: http.StatusUnauthorized,
			expectBody:     "",
			expectError:    true, // Middleware should return an error
		},
		{
			name: "Invalid Signature",
			tokenFunc: func() string {
				claims := &Claims{ // Use claims but sign with wrong key
					Email: "test@example.com",
					RegisteredClaims: jwt.RegisteredClaims{
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
					},
				}
				token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				tokenString, _ := token.SignedString([]byte("wrong-secret-key")) // Sign with wrong key
				return tokenString
			},
			expectedStatus: http.StatusUnauthorized,
			expectBody:     "",
			expectError:    true,
		},
		{
			name: "No Token",
			tokenFunc: func() string {
				return "" // No token provided
			},
			expectedStatus: http.StatusUnauthorized, // Assuming default behavior if no token
			expectBody:     "",
			expectError:    true, // Middleware should error if no token but expected
		},
		{
			name: "Malformed Token",
			tokenFunc: func() string {
				return "this.is.not.a.jwt"
			},
			expectedStatus: http.StatusUnauthorized,
			expectBody:     "",
			expectError:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup request and recorder for this test case
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			tokenString := tc.tokenFunc()
			if tokenString != "" {
				req.Header.Set(echo.HeaderAuthorization, "Bearer "+tokenString)
			}
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			// Execute the handler with middleware
			err := handlerWithMiddleware(c)

			// Assert: Check error expectation
			if tc.expectError {
				assert.Error(t, err, "Expected an error for "+tc.name)
				// Check if it's an HTTPError and the status code matches
				httpErr, ok := err.(*echo.HTTPError)
				assert.True(t, ok, "Error should be an echo.HTTPError")
				assert.Equal(t, tc.expectedStatus, httpErr.Code, "HTTP status code should match expected")
			} else {
				assert.NoError(t, err, "Did not expect an error for "+tc.name)
				assert.Equal(t, tc.expectedStatus, rec.Code, "HTTP status code should match expected")
				if tc.expectBody != "" {
					assert.Equal(t, tc.expectBody, rec.Body.String(), "Response body should match expected")
				}
			}
		})
	}
}
