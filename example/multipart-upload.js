const fs = require('fs');
const crypto = require('crypto');

const CHUNK_SIZE = 80 * 1024 * 1024; // 80MB per part

/**
 * Initialize a multi-part upload session
 * 
 * @param {string} token - Authentication token
 * @param {string} location - S3 object key where video will be stored
 * @param {number} fileSize - Total file size in bytes
 * @param {string} contentType - Video MIME type
 * @param {number} deadline - Unix timestamp when upload expires
 * @param {number} chunkSize - Optional chunk size (default: 80MB)
 * @returns {Promise<object>} Upload initialization response
 */
async function initializeMultipartUpload(token, location, fileSize, contentType, deadline, chunkSize = CHUNK_SIZE) {
    const url = new URL('http://localhost:3000/videos/multiparts');
    url.searchParams.set('token', token);
    url.searchParams.set('deadline', deadline);
    url.searchParams.set('location', location);
    url.searchParams.set('size', fileSize);
    url.searchParams.set('contentType', contentType);
    url.searchParams.set('chunkSize', chunkSize);

    const response = await fetch(url, { method: 'POST' });
    
    if (!response.ok) {
        const error = await response.text();
        throw new Error(`Failed to initialize upload: ${error}`);
    }

    return await response.json();
}

/**
 * Upload a single part of a multi-part upload
 * 
 * @param {string} token - Authentication token
 * @param {string} uploadId - Upload ID from initialization
 * @param {number} partIndex - Zero-based part index
 * @param {Buffer} partData - Part data buffer
 * @returns {Promise<object>} Upload part response
 */
async function uploadPart(token, uploadId, partIndex, partData) {
    const FormData = require('form-data');
    const formData = new FormData();
    formData.append('video', partData, { filename: `part${partIndex}.mp4` });

    const url = new URL(`http://localhost:3000/videos/multiparts/${uploadId}/parts/${partIndex}`);
    url.searchParams.set('token', token);

    const response = await fetch(url, {
        method: 'POST',
        body: formData,
        headers: formData.getHeaders()
    });

    if (!response.ok) {
        const error = await response.text();
        throw new Error(`Failed to upload part ${partIndex}: ${error}`);
    }

    return await response.json();
}

/**
 * Check the status of a multi-part upload
 * 
 * @param {string} token - Authentication token
 * @param {string} uploadId - Upload ID from initialization
 * @returns {Promise<object>} Upload status
 */
async function getUploadStatus(token, uploadId) {
    const url = new URL(`http://localhost:3000/videos/multiparts/${uploadId}`);
    url.searchParams.set('token', token);

    const response = await fetch(url);

    if (!response.ok) {
        const error = await response.text();
        throw new Error(`Failed to get upload status: ${error}`);
    }

    return await response.json();
}

/**
 * Upload a video file using multi-part upload
 * 
 * @param {string} filePath - Path to the video file
 * @param {string} location - S3 object key where video will be stored
 * @param {string} token - Authentication token
 * @param {function} onProgress - Optional progress callback
 * @returns {Promise<object>} Final upload status
 */
async function uploadVideoMultipart(filePath, location, token, onProgress = null) {
    // Get file stats
    const stats = fs.statSync(filePath);
    const fileSize = stats.size;
    const contentType = 'video/mp4'; // Adjust based on file type
    
    // Set deadline (5 hours from now)
    const deadline = Math.floor(Date.now() / 1000) + (5 * 3600);

    console.log('Starting multi-part upload...');
    console.log(`File: ${filePath}`);
    console.log(`Size: ${(fileSize / (1024 * 1024)).toFixed(2)} MB`);
    console.log(`Location: ${location}`);
    console.log(`Deadline: ${new Date(deadline * 1000).toISOString()}`);

    // Step 1: Initialize upload
    const uploadInfo = await initializeMultipartUpload(
        token,
        location,
        fileSize,
        contentType,
        deadline
    );

    console.log(`\nUpload initialized:`);
    console.log(`  Upload ID: ${uploadInfo.uploadId}`);
    console.log(`  Total parts: ${uploadInfo.partsCount}`);
    console.log(`  Chunk size: ${(uploadInfo.chunkSize / (1024 * 1024)).toFixed(2)} MB`);

    // Step 2: Upload each part
    const fd = fs.openSync(filePath, 'r');
    
    try {
        for (const part of uploadInfo.parts) {
            console.log(`\nUploading part ${part.index + 1}/${uploadInfo.partsCount}...`);
            console.log(`  Offset: ${part.offset}, Size: ${(part.size / (1024 * 1024)).toFixed(2)} MB`);
            
            // Read chunk from file
            const buffer = Buffer.alloc(part.size);
            fs.readSync(fd, buffer, 0, part.size, part.offset);
            
            // Upload part
            const result = await uploadPart(token, uploadInfo.uploadId, part.index, buffer);
            
            console.log(`  ✓ Part ${part.index + 1} uploaded`);
            console.log(`  Progress: ${result.complete ? '100%' : `${((part.index + 1) / uploadInfo.partsCount * 100).toFixed(1)}%`}`);
            
            if (onProgress) {
                onProgress({
                    partIndex: part.index,
                    partsCount: uploadInfo.partsCount,
                    complete: result.complete,
                    uploadedBytes: (part.index + 1) * uploadInfo.chunkSize
                });
            }
        }
    } finally {
        fs.closeSync(fd);
    }

    // Step 3: Verify upload completion
    console.log('\nVerifying upload...');
    const status = await getUploadStatus(token, uploadInfo.uploadId);

    console.log(`\n✓ Upload complete!`);
    console.log(`  Location: ${status.location}`);
    console.log(`  Total size: ${(status.totalSize / (1024 * 1024)).toFixed(2)} MB`);
    console.log(`  Parts uploaded: ${status.uploadedCount}/${status.partsCount}`);
    console.log(`  Progress: ${status.progress}%`);

    return status;
}

/**
 * Calculate parts information for a file
 * This matches the logic from your example
 */
function calculateParts(size, chunkSize = CHUNK_SIZE) {
    const partsCount = Math.ceil(size / chunkSize);
    const partSizes = [];
    const partOffsets = [];
    
    let offset = 0;
    for (let i = 0; i < partsCount; i++) {
        if (i === partsCount - 1) {
            // Last part: calculate remaining size
            const remainingSize = size - i * chunkSize;
            partSizes.push(remainingSize);
            partOffsets.push(offset);
            offset += remainingSize;
        } else {
            // All other parts: use full chunk size
            partSizes.push(chunkSize);
            partOffsets.push(offset);
            offset += chunkSize;
        }
    }

    return {
        parts: partsCount,
        partSizes,
        partOffsets,
    };
}

// Example usage
if (require.main === module) {
    const filePath = process.argv[2] || './test-video.mp4';
    const location = process.argv[3] || 'videos/test/my-video.mp4';
    const token = process.env.APP_TOKEN || 'your-upload-token';

    if (!fs.existsSync(filePath)) {
        console.error(`Error: File not found: ${filePath}`);
        process.exit(1);
    }

    uploadVideoMultipart(filePath, location, token, (progress) => {
        // Optional: handle progress updates
        console.log(`    Progress callback: ${progress.partIndex + 1}/${progress.partsCount} parts`);
    })
        .then(status => {
            console.log('\n✓ Success!');
            process.exit(0);
        })
        .catch(err => {
            console.error('\n✗ Error:', err.message);
            process.exit(1);
        });
}

module.exports = {
    initializeMultipartUpload,
    uploadPart,
    getUploadStatus,
    uploadVideoMultipart,
    calculateParts,
    CHUNK_SIZE
};
