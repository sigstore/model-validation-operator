FROM busybox

# Copy the real TensorFlow SavedModel files from testdata
COPY tensorflow_saved_model/ /data/

# Copy public keys for verification tests - separate directory
RUN mkdir -p /keys
COPY docker/test_public_key.pub /keys/test_public_key.pub
COPY docker/test_invalid_public_key.pub /keys/test_invalid_public_key.pub

# Make files readable and ensure no stray public key files in model directory
RUN chmod -R 644 /data /keys && rm -f /data/test_public_key.pub /data/*.pub

# Default command
CMD ["sleep", "3600"]