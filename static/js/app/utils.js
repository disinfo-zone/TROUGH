/**
 * Returns the correct image URL, handling both local and remote paths.
 * @param {string} filename - The filename or URL of the image.
 * @returns {string} The full URL for the image.
 */
export function getImageURL(filename) {
    if (!filename) return '';

    // Check for full URLs with protocol
    if (filename.startsWith('http://') || filename.startsWith('https://')) {
        return filename;
    }

    // Check for domain-based URLs without protocol (like z.disinfo.zone/file.jpg)
    if (filename.includes('.') && filename.includes('/') && !filename.startsWith('/')) {
        return 'https://' + filename;
    }

    // Local filename
    return `/uploads/${filename}`;
}

/**
 * Sanitizes and renders a simple subset of Markdown.
 * @param {string} md - The Markdown text to render.
 * @returns {string} The rendered HTML.
 */
export function sanitizeAndRenderMarkdown(md) {
    const maxLen = 2000;
    let text = (md || '').slice(0, maxLen);
    // Escape HTML
    text = text.replace(/[&<>"']/g, (ch) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;','\'':'&#39;'}[ch]));
    // Links [text](url)
    text = text.replace(/\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g, (m, label, url) => {
        try {
            const u = new URL(url);
            const safe = ['http:', 'https:'].includes(u.protocol);
            return safe ? `<a href="${u.href}" target="_blank" rel="noopener noreferrer">${label}</a>` : label;
        } catch { return label; }
    });
    // Bold **text** and italic *text*
    text = text.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    text = text.replace(/\*([^*]+)\*/g, '<em>$1</em>');
    // Line breaks
    text = text.replace(/\n/g, '<br>');
    return text;
}

/**
 * Escapes HTML special characters in a string.
 * @param {string} s - The string to escape.
 * @returns {string} The escaped string.
 */
export function escapeHTML(s){ return (s||'').replace(/[&<>"]/g, c=>({ '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;' }[c])); }
