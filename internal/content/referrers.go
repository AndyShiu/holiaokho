package content

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// Referrer is one artifact that declares another as its subject — a signature,
// an SBOM, an attestation.
type Referrer struct {
	Digest       string
	MediaType    string
	ArtifactType string
	Size         int64
	Annotations  map[string]string
}

// PutReferrer records that manifestDigest points at subjectDigest.
//
// Called on the manifest push path, so it stays to a single upsert with no
// reads: the overwhelming majority of manifests carry no subject at all and
// never reach this function, and the ones that do should not pay for a
// round-trip to find out whether the row already exists.
func (s *Service) PutReferrer(ctx context.Context, repoID uuid.UUID, imageName, subjectDigest, manifestDigest string, r Referrer) error {
	ann, err := json.Marshal(r.Annotations)
	if err != nil || r.Annotations == nil {
		ann = []byte("{}")
	}
	_, err = s.DB.Pool.Exec(ctx, `
		INSERT INTO oci_referrers
			(repo_id, image_name, subject_digest, manifest_digest, artifact_type, media_type, size, annotations)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (repo_id, image_name, subject_digest, manifest_digest) DO UPDATE
		SET artifact_type = EXCLUDED.artifact_type,
		    media_type    = EXCLUDED.media_type,
		    size          = EXCLUDED.size,
		    annotations   = EXCLUDED.annotations`,
		repoID, imageName, subjectDigest, manifestDigest, r.ArtifactType, r.MediaType, r.Size, ann)
	return err
}

// Referrers lists what points at subjectDigest, newest first.
//
// artifactType filters when non-empty; the caller has to tell the client it
// applied a filter, because the spec makes that part of the response.
func (s *Service) Referrers(ctx context.Context, repoID uuid.UUID, imageName, subjectDigest, artifactType string) ([]Referrer, error) {
	q := `
		SELECT manifest_digest, media_type, artifact_type, size, annotations
		FROM oci_referrers
		WHERE repo_id = $1 AND image_name = $2 AND subject_digest = $3`
	args := []any{repoID, imageName, subjectDigest}
	if artifactType != "" {
		q += ` AND artifact_type = $4`
		args = append(args, artifactType)
	}
	// Newest first, and capped: a subject with thousands of attestations
	// should not turn one request into an unbounded response.
	q += ` ORDER BY created_at DESC LIMIT 1000`

	rows, err := s.DB.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Referrer
	for rows.Next() {
		var r Referrer
		var ann []byte
		if err := rows.Scan(&r.Digest, &r.MediaType, &r.ArtifactType, &r.Size, &ann); err != nil {
			return nil, err
		}
		if len(ann) > 0 {
			json.Unmarshal(ann, &r.Annotations)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteReferrersOf removes the index entries for a manifest that is going
// away, in both directions: the rows saying it refers to something, and the
// rows saying something refers to it.
func (s *Service) DeleteReferrersOf(ctx context.Context, repoID uuid.UUID, imageName, manifestDigest string) error {
	_, err := s.DB.Pool.Exec(ctx, `
		DELETE FROM oci_referrers
		WHERE repo_id = $1 AND image_name = $2
		  AND (manifest_digest = $3 OR subject_digest = $3)`,
		repoID, imageName, manifestDigest)
	return err
}
