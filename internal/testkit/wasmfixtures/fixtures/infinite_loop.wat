;; Infinite Loop Adversarial Fixture (CT-17 / FMEA-05 Timeout & Cancel Watchdog)
;; Spins forever in a tight loop to verify WithCloseOnContextDone + pool eviction.
(module
  (memory (export "memory") 2 256)
  (func (export "transform") (param $ptr i32) (param $len i32) (result i64)
    (loop $spin
      (br $spin)
    )
    (i64.const 0)
  )
)
