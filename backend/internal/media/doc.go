// Package media owns media bytes and the byte-processing lifecycle.
//
// Media owns upload objects, immutable object versions, malware-scan evidence,
// transcoding evidence, and HLS rendition outputs. It deliberately does not
// own Course, Section, Lesson, publication, entitlement, or learning metadata;
// those boundaries remain in their owning packages. Callers pass opaque owner
// and content identifiers when a media operation needs to bind bytes to a
// higher-level record, but media does not import or decide higher-level Course
// behavior.
package media
