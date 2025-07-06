// Initialize WebAssembly
let wasmReady = false;

async function initWasm() {
    const go = new Go();
    try {
        const result = await WebAssembly.instantiateStreaming(
            fetch("/main.wasm"),
            go.importObject
        );
        go.run(result.instance);
        wasmReady = true;
    } catch (err) {
        console.error('Failed to load WASM:', err);
    }
}

initWasm();

// Token management functions
async function refreshToken() {
    try {
        // Get the current refresh token from localStorage
        const refreshToken = localStorage.getItem('refreshToken');
        if (!refreshToken) {
            // If no refresh token, force login
            localStorage.removeItem('token');
            showAuthSection();
            return false;
        }

        const response = await fetch('/api/refresh', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ refreshToken }),
        });

        if (response.ok) {
            const data = await response.json();
            localStorage.setItem('token', data.token);
            localStorage.setItem('refreshToken', data.refreshToken);
            return true;
        } else {
            // Token refresh failed, force login
            localStorage.removeItem('token');
            localStorage.removeItem('refreshToken');
            showAuthSection();
            return false;
        }
    } catch (error) {
        console.error('Token refresh error:', error);
        return false;
    }
}

async function revokeAllSessions() {
    try {
        const response = await fetch('/api/revoke-all', {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${localStorage.getItem('token')}`,
                'Content-Type': 'application/json',
            }
        });

        if (response.ok) {
            showSuccess('All sessions have been revoked. Please log in again.');
            // Clear local storage and force login
            localStorage.removeItem('token');
            localStorage.removeItem('refreshToken');
            delete window.arkfileSecurityContext;
            showAuthSection();
            return true;
        } else {
            showError('Failed to revoke sessions.');
            return false;
        }
    } catch (error) {
        console.error('Revoke sessions error:', error);
        showError('An error occurred while revoking sessions.');
        return false;
    }
}

// OPAQUE Authentication Functions

// Global state for device capability detection
window.arkfileOpaqueState = {
    deviceCapability: null,
    hasUserConsent: false,
    authMethod: 'OPAQUE'
};

// Privacy-first device capability detection
async function requestDeviceCapabilityConsent() {
    if (!wasmReady) {
        await initWasm();
    }

    const consentData = requestDeviceCapabilityPermission();
    
    return new Promise((resolve) => {
        // Create consent dialog
        const dialog = document.createElement('div');
        dialog.className = 'capability-consent-dialog modal';
        dialog.innerHTML = `
            <div class="modal-content">
                <h3>${consentData.title}</h3>
                <div class="consent-message">${consentData.message.replace(/\n/g, '<br>')}</div>
                <div class="consent-options">
                    <button onclick="handleCapabilityConsent('allow')" class="btn-primary">
                        ${consentData.options[0]}
                    </button>
                    <button onclick="handleCapabilityConsent('manual')" class="btn-secondary">
                        ${consentData.options[1]}
                    </button>
                    <button onclick="handleCapabilityConsent('maximum')" class="btn-secondary">
                        ${consentData.options[2]}
                    </button>
                </div>
            </div>
        `;
        
        document.body.appendChild(dialog);
        
        // Store resolver for use in button handlers
        window.capabilityConsentResolver = resolve;
    });
}

function handleCapabilityConsent(choice) {
    const dialog = document.querySelector('.capability-consent-dialog');
    if (dialog) {
        dialog.remove();
    }
    
    let capability;
    let hasConsent = false;
    
    switch (choice) {
        case 'allow':
            hasConsent = true;
            capability = detectDeviceCapabilityWithPermission(true);
            break;
        case 'manual':
            hasConsent = false;
            capability = 'interactive'; // Safe default
            break;
        case 'maximum':
            hasConsent = false;
            capability = 'maximum';
            break;
        default:
            hasConsent = false;
            capability = 'interactive';
    }
    
    window.arkfileOpaqueState.deviceCapability = capability;
    window.arkfileOpaqueState.hasUserConsent = hasConsent;
    
    if (window.capabilityConsentResolver) {
        window.capabilityConsentResolver(capability);
        delete window.capabilityConsentResolver;
    }
}

// Enhanced device capability detection with browser APIs
async function detectDeviceCapability() {
    if (window.arkfileOpaqueState.deviceCapability) {
        return window.arkfileOpaqueState.deviceCapability;
    }
    
    // Request user consent first
    const capability = await requestDeviceCapabilityConsent();
    
    // If user gave consent, enhance detection with browser APIs
    if (window.arkfileOpaqueState.hasUserConsent) {
        const deviceInfo = {
            memoryGB: navigator.deviceMemory || 0,
            cpuCores: navigator.hardwareConcurrency || 0,
            isMobile: /Android|iPhone|iPad|iPod|BlackBerry|IEMobile/i.test(navigator.userAgent),
            userAgent: navigator.userAgent
        };
        
        try {
            const response = await fetch('/api/opaque/capability', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify(deviceInfo),
            });
            
            if (response.ok) {
                const data = await response.json();
                window.arkfileOpaqueState.deviceCapability = data.recommendedCapability;
                showCapabilityInfo(data);
                return data.recommendedCapability;
            }
        } catch (error) {
            console.warn('Failed to get server capability recommendation:', error);
        }
    }
    
    return capability;
}

function showCapabilityInfo(capabilityData) {
    const info = document.createElement('div');
    info.className = 'capability-info success-message';
    info.innerHTML = `
        <div><strong>Security Level:</strong> ${capabilityData.recommendedCapability}</div>
        <div><strong>Description:</strong> ${capabilityData.description}</div>
        <div><strong>Source:</strong> ${capabilityData.source}</div>
    `;
    document.body.appendChild(info);
    setTimeout(() => info.remove(), 8000);
}

// OPAQUE Authentication functions (OPAQUE-only)
async function login() {
    const email = document.getElementById('login-email').value;
    const password = document.getElementById('login-password').value;

    if (!email || !password) {
        showError('Please enter both email and password.');
        return;
    }

    try {
        // Ensure WASM is ready
        if (!wasmReady) {
            await initWasm();
        }

        // Check OPAQUE health first
        const healthResponse = await fetch('/api/opaque/health');
        if (!healthResponse.ok) {
            showError('Authentication system unavailable. Please try again in a few moments.');
            return;
        }
        
        // Use OPAQUE authentication only
        await opaqueLogin(email, password);
        
    } catch (error) {
        console.error('Login error:', error);
        showError('An error occurred during login.');
    }
}

async function opaqueLogin(email, password) {
    try {
        showProgress('Authenticating with OPAQUE...');
        
        // Call OPAQUE login flow (placeholder for now)
        const opaqueResult = opaqueLoginFlow(email, password);
        
        if (!opaqueResult.success) {
            showError(opaqueResult.error || 'OPAQUE login failed');
            return;
        }
        
        // Make OPAQUE login request to server
        const response = await fetch('/api/opaque/login', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ email, password }),
        });

        if (response.ok) {
            const data = await response.json();
            
            // Store tokens
            localStorage.setItem('token', data.token);
            if (data.refreshToken) {
                localStorage.setItem('refreshToken', data.refreshToken);
            }
            
            // Store OPAQUE session key from server response
            if (data.sessionKey) {
                const sessionKeyBytes = atob(data.sessionKey);
                window.arkfileSecurityContext = {
                    sessionKey: data.sessionKey, // Keep as base64 for file encryption
                    authMethod: 'OPAQUE',
                    expiresAt: Date.now() + (60 * 60 * 1000) // 1 hour
                };
            }
            
            hideProgress();
            showSuccess(`Authenticated with ${data.authMethod}`);
            showFileSection();
            loadFiles();
        } else {
            hideProgress();
            const errorData = await response.json().catch(() => ({}));
            
            // Check if this is an approval-related error
            if (errorData.message && errorData.message.includes('not approved')) {
                // Fetch admin emails for contact info
                await fetchAdminEmails();
                
                const adminContactList = adminEmails.length > 0 
                    ? adminEmails.join('\n• ') 
                    : 'admin@arkfile.demo';
                
                createModal({
                    title: "Account Approval Required",
                    message: `🔒 Your account status: ${errorData.userStatus || 'Pending Approval'}

Your account is registered but requires administrator approval before you can log in.

📧 Contact an administrator for approval or status updates:
• ${adminContactList}

📅 Registration Date: ${errorData.registrationDate ? new Date(errorData.registrationDate).toLocaleDateString() : 'Unknown'}

💡 Tip: Include your registered email address (${email}) when contacting support.

⏰ Account approvals are typically processed within 1-2 business days.`
                });
            } else {
                showError(errorData.message || 'OPAQUE login failed. Please check your credentials.');
            }
        }
    } catch (error) {
        hideProgress();
        console.error('OPAQUE login error:', error);
        showError('OPAQUE authentication failed.');
    }
}

async function legacyLogin(email, password) {
    try {
        showProgress('Authenticating...');
        
        // Get user salt for legacy authentication
        const saltResponse = await fetch('/api/salt', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ email }),
        });

        if (!saltResponse.ok) {
            hideProgress();
            showError('Login failed. Please check your credentials.');
            return;
        }

        const saltData = await saltResponse.json();
        const salt = saltData.salt;

        // Hash password using Argon2ID
        const passwordHash = hashPasswordArgon2ID(password, salt);

        // Attempt legacy login
        const response = await fetch('/api/login', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ email, passwordHash }),
        });

        if (response.ok) {
            const data = await response.json();
            localStorage.setItem('token', data.token);
            
            if (data.refreshToken) {
                localStorage.setItem('refreshToken', data.refreshToken);
            }
            
            // Generate session key for legacy authentication
            const sessionSalt = generateSalt();
            const sessionKey = deriveSessionKey(password, sessionSalt);
            
            window.arkfileSecurityContext = {
                sessionKey: sessionKey,
                sessionSalt: sessionSalt,
                authMethod: 'Legacy',
                expiresAt: Date.now() + (60 * 60 * 1000)
            };
            
            hideProgress();
            showSuccess('Authenticated with Legacy method');
            showFileSection();
            loadFiles();
        } else {
            hideProgress();
            showError('Login failed. Please check your credentials.');
        }
    } catch (error) {
        hideProgress();
        console.error('Legacy login error:', error);
        showError('Authentication failed.');
    }
}

async function register() {
    const email = document.getElementById('register-email').value;
    const password = document.getElementById('register-password').value;
    const confirmPassword = document.getElementById('register-password-confirm').value;

    if (password !== confirmPassword) {
        showError('Passwords do not match.');
        return;
    }

    // Validate password complexity
    if (!wasmReady) {
        await initWasm();
    }
    
    const validation = validatePasswordComplexity(password);
    if (!validation.valid) {
        showError(validation.message);
        return;
    }

    try {
        // Check OPAQUE health first
        const healthResponse = await fetch('/api/opaque/health');
        if (!healthResponse.ok) {
            showError('Authentication system unavailable. Please try again in a few moments.');
            return;
        }
        
        // Use OPAQUE registration only
        await opaqueRegister(email, password);
        
    } catch (error) {
        console.error('Registration error:', error);
        showError('An error occurred during registration.');
    }
}

async function opaqueRegister(email, password) {
    try {
        showProgress('Detecting device capability...');
        
        // Get device capability with user consent
        const deviceCapability = await detectDeviceCapability();
        
        showProgress('Registering with OPAQUE...');
        
        // Call OPAQUE registration flow (placeholder for now)
        const opaqueResult = opaqueRegisterFlow(email, password, deviceCapability);
        
        if (!opaqueResult.success) {
            hideProgress();
            showError(opaqueResult.error || 'OPAQUE registration preparation failed');
            return;
        }
        
        // Make OPAQUE registration request to server
        const response = await fetch('/api/opaque/register', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ 
                email, 
                password, 
                deviceCapability 
            }),
        });

        if (response.ok) {
            const data = await response.json();
            hideProgress();
            
            // Fetch admin emails for contact info
            await fetchAdminEmails();
            
            // Show registration success modal with approval information
            const adminContactList = adminEmails.length > 0 
                ? adminEmails.join('\n• ') 
                : 'admin@arkfile.demo';
                
            createModal({
                title: "Registration Successful!",
                message: `Your account has been created successfully with ${data.authMethod}.

📋 Next Steps:
• An administrator must approve your account before you can log in
• You will receive an email notification when approved
• This usually takes 1-2 business days

📧 Need help or want to check your status?
Contact an administrator:
• ${adminContactList}

💡 Tip: Include your registered email address (${email}) when contacting support.`
            });
            
            toggleAuthForm();
        } else {
            hideProgress();
            const errorData = await response.json().catch(() => ({}));
            
            createModal({
                title: "Registration Failed",
                message: `Registration was unsuccessful.

Error: ${errorData.message || 'OPAQUE registration failed. Please try again.'}

Please check your information and try again. If the problem persists, contact an administrator.`
            });
        }
    } catch (error) {
        hideProgress();
        console.error('OPAQUE registration error:', error);
        showError('OPAQUE registration failed.');
    }
}

async function legacyRegister(email, password) {
    try {
        showProgress('Registering...');
        
        // Generate salt and hash password client-side
        const salt = generatePasswordSalt();
        const passwordHash = hashPasswordArgon2ID(password, salt);

        const response = await fetch('/api/register', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ email, passwordHash, salt }),
        });

        if (response.ok) {
            hideProgress();
            showSuccess('Registration successful with Legacy method. Please login.');
            toggleAuthForm();
        } else {
            hideProgress();
            showError('Legacy registration failed. Please try again.');
        }
    } catch (error) {
        hideProgress();
        console.error('Legacy registration error:', error);
        showError('Legacy registration failed.');
    }
}

// Progress indicator functions
function showProgress(message) {
    let progressDiv = document.getElementById('progress-indicator');
    if (!progressDiv) {
        progressDiv = document.createElement('div');
        progressDiv.id = 'progress-indicator';
        progressDiv.className = 'progress-message';
        document.body.appendChild(progressDiv);
    }
    progressDiv.textContent = message;
    progressDiv.style.display = 'block';
}

function hideProgress() {
    const progressDiv = document.getElementById('progress-indicator');
    if (progressDiv) {
        progressDiv.style.display = 'none';
    }
}

// This function is now handled by securityUtils.updatePasswordStrengthUI
// Remove the old broken implementation

// File handling functions
async function uploadFile() {
    if (!wasmReady) {
        showError('WASM not ready. Please try again.');
        return;
    }

    const fileInput = document.getElementById('fileInput');
    const file = fileInput.files[0];
    if (!file) {
        showError('Please select a file.');
        return;
    }

    const useCustomPassword = document.getElementById('useCustomPassword').checked;
    let password;
    let keyType;

    if (useCustomPassword) {
        password = document.getElementById('filePassword').value;
        const passwordValidation = securityUtils.validatePassword(password);
        if (!passwordValidation.valid) {
            showError(passwordValidation.message);
            return;
        }
        keyType = 'custom';
    } else {
        // Use the session key
        const securityContext = window.arkfileSecurityContext;
        
        if (!securityContext || Date.now() > securityContext.expiresAt) {
            showError('Your session has expired. Please log in again.');
            localStorage.removeItem('token');
            showAuthSection();
            return;
        }
        
        password = securityContext.sessionKey;
        keyType = 'account';
    }

    const passwordHint = document.getElementById('passwordHint').value;

    try {
        // Read file as ArrayBuffer
        const fileData = await file.arrayBuffer();
        const fileBytes = new Uint8Array(fileData);
        
        // Calculate SHA-256 hash of original file
        const sha256sum = calculateSHA256(fileBytes);
        
        // Encrypt the file
        const encryptedData = encryptFile(fileBytes, password, keyType);

        // Prepare form data
        const formData = new FormData();
        formData.append('filename', file.name);
        formData.append('data', encryptedData);
        formData.append('passwordHint', passwordHint);
        formData.append('passwordType', keyType);
        formData.append('sha256sum', sha256sum);

        // Upload to server
        const response = await fetch('/api/upload', {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${localStorage.getItem('token')}`,
            },
            body: formData,
        });

        if (response.ok) {
            showSuccess('File uploaded successfully.');
            loadFiles();
        } else {
            showError('Failed to upload file.');
        }
    } catch (error) {
        console.error('Upload error:', error);
        showError('An error occurred during file upload.');
    }
}

async function downloadFile(filename, hint, expectedHash, passwordType) {
    if (!wasmReady) {
        showError('WASM not ready. Please try again.');
        return;
    }

    if (hint) {
        alert(`Password Hint: ${hint}`);
    }

    let password;
    
    if (passwordType === 'account') {
        // For account-encrypted files, use the session key
        const securityContext = window.arkfileSecurityContext;
        
        if (!securityContext || Date.now() > securityContext.expiresAt) {
            showError('Your session has expired. Please log in again to decrypt account-encrypted files.');
            return;
        }
        
        password = securityContext.sessionKey;
    } else {
        // For custom password-encrypted files
        password = prompt('Enter the file password:');
        if (!password) return;
    }

    try {
        const response = await fetch(`/api/download/${filename}`, {
            headers: {
                'Authorization': `Bearer ${localStorage.getItem('token')}`,
            },
        });

        if (response.ok) {
            const data = await response.json();
            const decryptedData = decryptFile(data.data, password);

            if (decryptedData === 'Failed to decrypt data') {
                showError('Incorrect password or corrupted file.');
                return;
            }

            // Convert base64 to Uint8Array for hash verification
            const decryptedBytes = Uint8Array.from(atob(decryptedData), c => c.charCodeAt(0));
            
            // Verify file integrity
            const calculatedHash = calculateSHA256(decryptedBytes);
            if (calculatedHash !== expectedHash) {
                showError('File integrity check failed. The file may be corrupted.');
                return;
            }

            // Create and download the file
            const blob = new Blob([decryptedBytes]);
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = filename;
            a.click();
            URL.revokeObjectURL(url);
        } else {
            showError('Failed to download file.');
        }
    } catch (error) {
        showError('An error occurred during file download.');
    }
}

async function loadFiles() {
    try {
        const response = await fetch('/api/files', {
            headers: {
                'Authorization': `Bearer ${localStorage.getItem('token')}`,
            },
        });

        if (response.ok) {
            const data = await response.json();
            const filesList = document.getElementById('filesList');
            filesList.innerHTML = '';

            data.files.forEach(file => {
                const fileElement = document.createElement('div');
                fileElement.className = 'file-item';
                fileElement.innerHTML = `
                    <div class="file-info">
                        <strong>${file.filename}</strong>
                        <span class="file-size">${file.size_readable}</span>
                        <span class="file-date">${new Date(file.uploadDate).toLocaleString()}</span>
                        <span class="encryption-type">${file.passwordType === 'account' ? '🔑 Account Password' : '🔒 Custom Password'}</span>
                    </div>
                    <div class="file-actions">
                        <button onclick="downloadFile('${file.filename}', '${file.passwordHint}', '${file.sha256sum}', '${file.passwordType}')">Download</button>
                    </div>
                `;
                filesList.appendChild(fileElement);
            });

            // Update storage info
            updateStorageInfo(data.storage);
        } else {
            showError('Failed to load files.');
        }
    } catch (error) {
        showError('An error occurred while loading files.');
    }
}

function updateStorageInfo(storage) {
    const storageInfo = document.getElementById('storageInfo');
    if (!storageInfo) return;

    storageInfo.innerHTML = `
        <div class="storage-bar">
            <div class="used" style="width: ${storage.usage_percent}%"></div>
        </div>
        <div class="storage-text">
            Used: ${storage.total_readable} of ${storage.limit_readable} (${storage.usage_percent.toFixed(1)}%)
        </div>
    `;
}

// Logout function
async function logout() {
    try {
        // Get the current refresh token
        const refreshToken = localStorage.getItem('refreshToken');
        
        if (refreshToken) {
            // Call the logout API to revoke the refresh token
            await fetch('/api/logout', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ refreshToken }),
            });
        }
        
        // Clear local storage
        localStorage.removeItem('token');
        localStorage.removeItem('refreshToken');
        
        // Clear session key from memory
        delete window.arkfileSecurityContext;
        
        // Show auth section
        showAuthSection();
        
        showSuccess('Logged out successfully.');
    } catch (error) {
        console.error('Logout error:', error);
        // Still clear local storage and redirect
        localStorage.removeItem('token');
        localStorage.removeItem('refreshToken');
        showAuthSection();
    }
}

// Function to toggle security settings panel
function toggleSecuritySettings() {
    const securityPanel = document.getElementById('security-settings');
    if (securityPanel) {
        securityPanel.classList.toggle('hidden');
    }
}

// Modal utility functions
function createModal(options) {
    const modal = document.createElement('div');
    modal.className = 'modal-overlay';
    modal.style.cssText = `
        position: fixed;
        top: 0;
        left: 0;
        width: 100%;
        height: 100%;
        background-color: rgba(0, 0, 0, 0.5);
        display: flex;
        justify-content: center;
        align-items: center;
        z-index: 1000;
    `;

    const modalContent = document.createElement('div');
    modalContent.className = 'modal-content';
    modalContent.style.cssText = `
        background: white;
        padding: 30px;
        border-radius: 8px;
        max-width: 500px;
        width: 90%;
        max-height: 80vh;
        overflow-y: auto;
        box-shadow: 0 4px 20px rgba(0, 0, 0, 0.3);
    `;

    const title = document.createElement('h3');
    title.style.cssText = `
        margin-top: 0;
        margin-bottom: 15px;
        color: #333;
        text-align: center;
    `;
    title.textContent = options.title;

    const message = document.createElement('div');
    message.style.cssText = `
        margin-bottom: 20px;
        line-height: 1.5;
        color: #666;
        white-space: pre-line;
    `;
    message.textContent = options.message;

    const closeButton = document.createElement('button');
    closeButton.textContent = 'Close';
    closeButton.style.cssText = `
        width: 100%;
        padding: 10px;
        background-color: #007bff;
        color: white;
        border: none;
        border-radius: 4px;
        cursor: pointer;
        font-size: 16px;
    `;
    closeButton.onclick = () => modal.remove();

    modalContent.appendChild(title);
    modalContent.appendChild(message);
    modalContent.appendChild(closeButton);
    modal.appendChild(modalContent);

    // Close modal when clicking outside
    modal.onclick = (e) => {
        if (e.target === modal) {
            modal.remove();
        }
    };

    document.body.appendChild(modal);
    return modal;
}

// Get admin emails from server
let adminEmails = [];
async function fetchAdminEmails() {
    try {
        const response = await fetch('/api/admin-contacts');
        if (response.ok) {
            const data = await response.json();
            adminEmails = data.adminEmails || [];
        }
    } catch (error) {
        console.warn('Could not fetch admin emails:', error);
        adminEmails = ['admin@arkfile.demo']; // Fallback
    }
}

// UI helper functions
function toggleAuthForm() {
    document.getElementById('login-form').classList.toggle('hidden');
    document.getElementById('register-form').classList.toggle('hidden');
}

function showFileSection() {
    document.getElementById('auth-section').classList.add('hidden');
    document.getElementById('file-section').classList.remove('hidden');
}

function showAuthSection() {
    document.getElementById('auth-section').classList.remove('hidden');
    document.getElementById('file-section').classList.add('hidden');
}

function showError(message) {
    const errorDiv = document.createElement('div');
    errorDiv.className = 'error-message';
    errorDiv.textContent = message;
    document.body.appendChild(errorDiv);
    setTimeout(() => errorDiv.remove(), 5000);
}

function showSuccess(message) {
    const successDiv = document.createElement('div');
    successDiv.className = 'success-message';
    successDiv.textContent = message;
    document.body.appendChild(successDiv);
    setTimeout(() => successDiv.remove(), 5000);
}

// Event listeners
window.addEventListener('load', () => {
    if (localStorage.getItem('token')) {
        showFileSection();
        loadFiles();
    }
    
    // Set up password type toggle handling
    const passwordTypeRadios = document.querySelectorAll('input[name="passwordType"]');
    const customPasswordSection = document.getElementById('customPasswordSection');
    const filePassword = document.getElementById('filePassword');
    
    passwordTypeRadios.forEach(radio => {
        radio.addEventListener('change', (e) => {
            const useCustomPassword = e.target.value === 'custom';
            customPasswordSection.classList.toggle('hidden', !useCustomPassword);
            if (!useCustomPassword) {
                filePassword.value = ''; // Clear password field when switching to account password
            }
        });
    });

    // Setup password strength monitoring for registration
    const registerPassword = document.getElementById('register-password');
    const registerContainer = document.querySelector('.register-form .password-section');
    if (registerPassword && registerContainer) {
        registerPassword.addEventListener('input', (e) => {
            securityUtils.updatePasswordStrengthUI(e.target.value, registerContainer);
        });
    }

    // Setup password strength monitoring for file upload
    if (filePassword && customPasswordSection) {
        filePassword.addEventListener('input', (e) => {
            securityUtils.updatePasswordStrengthUI(e.target.value, customPasswordSection);
        });
    }
});

// Check session validity periodically
setInterval(() => {
    const securityContext = window.arkfileSecurityContext;
    if (securityContext && Date.now() > securityContext.expiresAt) {
        delete window.arkfileSecurityContext;
    }
}, 60000); // Check every minute
