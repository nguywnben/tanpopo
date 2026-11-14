;; Unapproved WASI Filesystem Import Adversarial Fixture (TP-FR-0303 / Zero-FS Sandbox)
;; Attempts to import wasi_snapshot_preview1.path_open so a default-deny Wasm host
;; rejects module instantiation prior to executing any guest code.
(module
  (import "wasi_snapshot_preview1" "path_open"
    (func $path_open (param i32 i32 i32 i32 i32 i64 i64 i32 i32) (result i32))
  )
  (memory (export "memory") 2 256)
  (func (export "transform") (param $ptr i32) (param $len i32) (result i64)
    (drop (call $path_open
      (i32.const 0) (i32.const 0) (i32.const 0) (i32.const 0)
      (i32.const 0) (i64.const 0) (i64.const 0) (i32.const 0) (i32.const 0)
    ))
    (i64.const 0)
  )
)
