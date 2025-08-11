// Node.js example: sign params with APP_HMAC_KEY for media-proxy
// Matches Go validation: HMAC-SHA256 over message, hex digest (lowercase)

const crypto = require('crypto');

// Read secret from env (same variable name used in Go config: APP_HMAC_KEY)
const APP_HMAC_KEY = process.env.APP_HMAC_KEY || 'change-me';

function hexHmacSha256(message, secret) {
  return crypto.createHmac('sha256', secret).update(message, 'utf8').digest('hex');
}

// For path-style API: last segment is base64 URL-encoded source URL
// Go uses base64.URLEncoding (URL-safe, with padding). Keep '=' padding.
function base64UrlEncode(str) {
  return Buffer.from(str, 'utf8')
    .toString('base64')
    .replace(/\+/g, '-')
    .replace(/\//g, '_');
  // NOTE: do NOT strip '=' padding; Go decoder expects padded URL encoding by default
}

// Build examples matching validation rules
function buildSignedExamples({ url, location }) {
  if (!APP_HMAC_KEY || APP_HMAC_KEY === 'change-me') {
    console.warn('[warn] APP_HMAC_KEY is not set; using a placeholder. Set APP_HMAC_KEY in your env.');
  }

  // Case 1: signature over URL only
  const sigForUrl = hexHmacSha256(url, APP_HMAC_KEY);

  const queryOnly = `/images?url=${encodeURIComponent(url)}&signature=${sigForUrl}`;
  const pathOnly = `/images/sig:${sigForUrl}/${base64UrlEncode(url)}`;

  // Case 2: custom location → signature over `${url}|${location}` (exactly one pipe)
  let queryWithLocation = null;
  if (location) {
    const message = `${url}|${location}`;
    const sigForUrlAndLoc = hexHmacSha256(message, APP_HMAC_KEY);
    queryWithLocation = `/images?url=${encodeURIComponent(url)}&location=${encodeURIComponent(location)}&signature=${sigForUrlAndLoc}`;
  }

  return { queryOnly, pathOnly, queryWithLocation };
}

// Demo
async function main() {
  const sourceUrl = 'https://example.com/path/to/cat.jpg';
  const customLocation = 'uploads/2025/08/cat.jpg'; // optional; must pass server-side validation

  const { queryOnly, pathOnly, queryWithLocation } = buildSignedExamples({
    url: sourceUrl,
    location: customLocation,
  });

  console.log('Signature key (APP_HMAC_KEY):', APP_HMAC_KEY ? '[set]' : '[missing]');
  console.log('— Query (URL only):', queryOnly);
  console.log('— Path  (URL only):', pathOnly);
  if (queryWithLocation) {
    console.log('— Query (URL + location):', queryWithLocation);
  }
}

main();

module.exports = { hexHmacSha256, base64UrlEncode, buildSignedExamples };
