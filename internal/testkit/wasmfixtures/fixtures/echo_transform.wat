;; Echo Transform Adversarial/Positive Fixture (POC-01 Linear-Memory Slice ABI)
;; Copies len bytes from ptr to linear memory offset 65536 and returns
;; packed uint64: (uint64(65536) << 32) | uint64(len).
(module
  (memory (export "memory") 2 256)
  (func (export "transform") (param $ptr i32) (param $len i32) (result i64)
    (memory.copy (i32.const 65536) (local.get $ptr) (local.get $len))
    (i64.or
      (i64.shl (i64.const 65536) (i64.const 32))
      (i64.extend_i32_u (local.get $len))
    )
  )
)
