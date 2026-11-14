;; 64 MiB Memory Exhaustion Adversarial Fixture (CT-17 / TP-NFR-0401)
;; Requests +1024 Wasm pages (+64 MiB). When Host caps at 256 pages (16 MiB),
;; memory.grow returns -1; "transform" traps on -1 while "grow_64m" returns -1.
(module
  (memory (export "memory") 1)
  (func (export "transform") (param $ptr i32) (param $len i32) (result i64)
    (if (i32.eq (memory.grow (i32.const 1024)) (i32.const -1))
      (then (unreachable))
    )
    (i64.const 0)
  )
  (func (export "grow_64m") (result i32)
    (memory.grow (i32.const 1024))
  )
)
