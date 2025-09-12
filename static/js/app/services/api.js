let csrfToken = null;
let csrfTokenPromise = null;

async function fetchCSRFToken() {
    if (csrfTokenPromise) {
        return csrfTokenPromise;
    }

    csrfTokenPromise = (async () => {
        try {
            const response = await fetch('/api/csrf', { credentials: 'include' });
            if (response.ok) {
                const data = await response.json();
                csrfToken = data.csrf_token;
                // Also extract from cookie as fallback
                if (!csrfToken) {
                    const cookie = document.cookie
                        .split('; ')
                        .find(row => row.startsWith('csrf_token='));
                    if (cookie) {
                        csrfToken = cookie.split('=')[1];
                    }
                }
            }
        } catch (error) {
            console.warn('Failed to fetch CSRF token:', error);
        }
        return csrfToken;
    })();

    return csrfTokenPromise;
}

export async function initCSRFToken() {
    // Always fetch CSRF token on app start, regardless of auth status
    await fetchCSRFToken();
}

export async function fetchWithCSRF(url, options = {}) {
    if (!options.method || ['GET', 'HEAD', 'OPTIONS'].includes(options.method.toUpperCase())) {
        return fetch(url, options);
    }

    // Skip CSRF for authentication endpoints
    if (url.includes('/api/register') ||
        url.includes('/api/login') ||
        url.includes('/api/logout') ||
        url.includes('/api/forgot-password') ||
        url.includes('/api/reset-password') ||
        url.includes('/api/verify-email') ||
        url.includes('/api/validate-invite') ||
        url.includes('/api/me/resend-verification') ||
        url.includes('/send-verification')) {
        return fetch(url, options);
    }

    // For state-changing requests, ensure we have a CSRF token
    if (!csrfToken) {
        await fetchCSRFToken();
    }

    const headers = {
        ...options.headers,
        'X-CSRF-Token': csrfToken
    };

    // For multipart forms, add CSRF token as form field
    if (options.body && options.body instanceof FormData) {
        options.body.append('csrf_token', csrfToken);
    }

    return fetch(url, {
        ...options,
        headers
    });
}
