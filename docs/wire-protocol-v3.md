# Bridge wire protocol v3

See `internal/wire/v3.go`. Semantic `receipt` map keys by `kind`:

- **order**: `order_id`, `table_number`, `table_location`, `tenant_name`, `created_at`, `items` (array of `{product_name, quantity, unit_price, extras?}`), `takeaway?`
- **pos_receipt**: full semantic POS receipt fields, or transitional `escpos_base64` for raw bytes passthrough.
- **kitchen**: `correlation_id`, `created_at`, `table_number`, `table_location`, `takeaway`, `items` (same shape as order lines).
- **service_call**: `table_number`, `tenant_name`, `reason` (`payment` | other).
- **logo_provision**: `logo_hash`, image bytes via `image_b64` in receipt or separate cloud field.
- **test**: `receipt` may be empty; bridge uses built-in test pattern.

Bridge → cloud unchanged: `print_ack`, `printer_status`, `order_printed`, `order_print_failed`, `update_status`, `submit_pairing_code`, `unpair_me`.
