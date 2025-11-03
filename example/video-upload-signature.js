const crypto = require('crypto');

/**
 * Generate a signature for video upload
 * 
 * @param {string} hmacKey - The HMAC key from APP_HMAC_KEY environment variable
 * @param {number} deadline - Unix timestamp when the upload should expire
 * @param {string} location - S3 object key where the video will be stored (e.g., "videos/my-video.mp4")
 * @returns {object} - Object containing the signature and base64-encoded location
 */
function generateVideoUploadSignature(hmacKey, deadline, location) {
    // Create the message to sign: deadline|location
    const message = `${deadline}|${location}`;
    
    // Generate HMAC-SHA256 signature
    const hmac = crypto.createHmac('sha256', hmacKey);
    hmac.update(message);
    const signature = hmac.digest('hex');
    
    // Encode location in base64 URL-safe format
    const locationEncoded = Buffer.from(location).toString('base64')
        .replace(/\+/g, '-')
        .replace(/\//g, '_')
        .replace(/=/g, '');
    
    return {
        signature,
        locationEncoded,
        deadline
    };
}

/**
 * Example usage
 */
function example() {
    const hmacKey = 'your-secret-hmac-key'; // From APP_HMAC_KEY environment variable
    
    // Set deadline to 5 minutes from now
    const deadline = Math.floor(Date.now() / 1000) + (5 * 60);
    
    // Location where the video will be stored in S3
    const location = 'videos/user123/my-video.mp4';
    
    const { signature, locationEncoded, deadline: uploadDeadline } = generateVideoUploadSignature(
        hmacKey,
        deadline,
        location
    );
    
    console.log('Video Upload Parameters:');
    console.log('------------------------');
    console.log('Deadline:', uploadDeadline);
    console.log('Location (encoded):', locationEncoded);
    console.log('Signature:', signature);
    console.log('\nUpload URL:');
    console.log(`POST http://localhost:8080/videos?deadline=${uploadDeadline}&location=${locationEncoded}&signature=${signature}`);
    console.log('\nForm Data:');
    console.log('- video: [your video file]');
    
    // Example using fetch
    console.log('\n\nExample using fetch:');
    console.log('```javascript');
    console.log(`const formData = new FormData();`);
    console.log(`formData.append('video', videoFile);`);
    console.log('');
    console.log(`const response = await fetch(`);
    console.log(`  'http://localhost:8080/videos?deadline=${uploadDeadline}&location=${locationEncoded}&signature=${signature}',`);
    console.log(`  {`);
    console.log(`    method: 'POST',`);
    console.log(`    body: formData`);
    console.log(`  }`);
    console.log(`);`);
    console.log('```');
}

// Run example if called directly
if (require.main === module) {
    example();
}

module.exports = {
    generateVideoUploadSignature
};
