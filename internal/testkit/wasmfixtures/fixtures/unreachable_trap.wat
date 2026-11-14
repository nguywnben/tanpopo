;; Unreachable Trap Adversarial Fixture (CT-17 / FMEA-05 Trap Isolation & Pool Recovery)
;; Executes an immediate WebAssembly unreachable instruction to force module tainting.
(module
  (memory (export "memory") 2 256)
  (func (export "transform") (param $ptr i32) (param $len i32) (result i64)
    (unreachable)
  )
)
