-- name: ListActiveCertificates :many
SELECT fingerprint, certificate_pem, private_key_pem FROM gateway_certificate WHERE status='active' AND not_after > clock_timestamp() ORDER BY not_after DESC;
