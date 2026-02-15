# Doc Glyph (▤)

Inline document viewer on canvas. Drag a file onto the canvas to upload it and spawn a Doc glyph.

## Supported formats

PDF, PNG, JPG, GIF, WebP, SVG, TXT — validated server-side by both file extension and detected MIME type.

## Storage

Files stored on disk at `{db_directory}/files/{uuid}{ext}`, served via `GET /api/files/{id}`. The glyph's `content` field stores a JSON reference:

```json
{ "fileId": "uuid", "filename": "original.pdf", "ext": ".pdf" }
```

## Known limitations

**Browser PDF toolbar**: Browsers render their native PDF toolbar inside the `<embed>` element (page navigation, zoom, annotation controls). There is no cross-browser way to suppress it — Chrome's `#toolbar=0` URL fragment is ignored by Firefox's pdf.js viewer. Replacing `<embed>` with pdf.js as a library would give full rendering control at the cost of a dependency and custom page navigation/zoom.

## Files

| File | Role |
|------|------|
| `web/ts/components/glyph/doc-glyph.ts` | Glyph factory |
| `web/ts/api/files.ts` | Upload helper + URL builder |
| `web/css/canvas.css` | `.canvas-doc-glyph`, `.doc-embed` styles |
| `server/files.go` | Upload/serve handlers |
| `server/files_test.go` | Security + round-trip tests |
| `glyph/proto/files.proto` | `FileUploadResult` boundary type |
