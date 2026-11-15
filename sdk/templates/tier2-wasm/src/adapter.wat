;; Tanpopo Tier-2 Sandboxed Wasm Provider Adapter Starter (WebAssembly 1.0)
;; 1:1 matching source for dist/adapter.wasm (0 WASI/socket/FS imports, <= 16 MiB linear memory).
(module
  (memory (export "memory") 4 256)

  (func $pack_slice (param i32) (param i32) (result i64)
    local.get 0
    i64.extend_i32_u
    i64.const 32
    i64.shl
    local.get 1
    i64.extend_i32_u
    i64.or
  )
  (func $memcpy_ret (param i32) (param i32) (param i32) (result i32)
    local.get 0
    local.get 1
    local.get 2
    memory.copy
    local.get 0
    local.get 2
    i32.add
  )
  (func $find_substr (param i32) (param i32) (param i32) (param i32) (result i32)
    (local i32 i32 i32)
    local.get 3
    i32.eqz
    if
      i32.const 0
      return
    end
    local.get 1
    local.get 3
    i32.lt_s
    if
      i32.const -1
      return
    end
    local.get 1
    local.get 3
    i32.sub
    local.set 6
    i32.const 0
    local.set 4
    block
      loop
        local.get 4
        local.get 6
        i32.gt_s
        br_if 1
        i32.const 0
        local.set 5
        block
          loop
            local.get 5
            local.get 3
            i32.ge_s
            if
              local.get 4
              return
            end
            local.get 0
            local.get 4
            i32.add
            local.get 5
            i32.add
            i32.load8_u
            local.get 2
            local.get 5
            i32.add
            i32.load8_u
            i32.ne
            br_if 1
            local.get 5
            i32.const 1
            i32.add
            local.set 5
            br 0
          end
        end
        local.get 4
        i32.const 1
        i32.add
        local.set 4
        br 0
      end
    end
    i32.const -1
  )
  (func $extract_json_str (param i32) (param i32) (param i32) (param i32) (param i32) (result i32)
    (local i32 i32 i32 i32 i32)
    local.get 0
    local.get 1
    local.get 2
    local.get 3
    call $find_substr
    local.tee 5
    i32.const 0
    i32.lt_s
    if
      i32.const -1
      return
    end
    local.get 0
    local.get 5
    i32.add
    local.get 3
    i32.add
    local.set 6
    local.get 0
    local.get 1
    i32.add
    local.set 7
    i32.const 0
    local.set 8
    block
      loop
        local.get 6
        local.get 7
        i32.ge_s
        br_if 1
        local.get 6
        i32.load8_u
        local.tee 9
        i32.const 34
        i32.eq
        br_if 1
        local.get 9
        i32.const 92
        i32.eq
        if
          local.get 6
          i32.const 1
          i32.add
          local.get 7
          i32.lt_s
          if
            local.get 6
            i32.const 1
            i32.add
            local.set 6
            local.get 6
            i32.load8_u
            local.set 9
          end
        end
        local.get 4
        local.get 8
        i32.add
        local.get 9
        i32.store8
        local.get 8
        i32.const 1
        i32.add
        local.set 8
        local.get 6
        i32.const 1
        i32.add
        local.set 6
        br 0
      end
    end
    local.get 8
  )
  (func $extract_json_int (param i32) (param i32) (param i32) (param i32) (result i32)
    (local i32 i32 i32 i32 i32 i32)
    local.get 0
    local.get 1
    local.get 2
    local.get 3
    call $find_substr
    local.tee 4
    i32.const 0
    i32.lt_s
    if
      i32.const -1
      return
    end
    local.get 0
    local.get 4
    i32.add
    local.get 3
    i32.add
    local.set 5
    local.get 0
    local.get 1
    i32.add
    local.set 6
    i32.const 0
    local.set 7
    i32.const 0
    local.set 9
    block
      loop
        local.get 5
        local.get 6
        i32.ge_s
        br_if 1
        local.get 5
        i32.load8_u
        local.tee 8
        i32.const 32
        i32.ne
        local.get 8
        i32.const 9
        i32.ne
        i32.and
        br_if 1
        local.get 5
        i32.const 1
        i32.add
        local.set 5
        br 0
      end
    end
    block
      loop
        local.get 5
        local.get 6
        i32.ge_s
        br_if 1
        local.get 5
        i32.load8_u
        local.tee 8
        i32.const 48
        i32.lt_s
        local.get 8
        i32.const 57
        i32.gt_s
        i32.or
        br_if 1
        i32.const 1
        local.set 9
        local.get 7
        i32.const 10
        i32.mul
        local.get 8
        i32.const 48
        i32.sub
        i32.add
        local.set 7
        local.get 5
        i32.const 1
        i32.add
        local.set 5
        br 0
      end
    end
    local.get 9
    if (result i32)
      local.get 7
    else
      i32.const -1
    end
  )
  (func $b64_val (param i32) (result i32)
    local.get 0
    i32.const 65
    i32.ge_s
    local.get 0
    i32.const 90
    i32.le_s
    i32.and
    if
      local.get 0
      i32.const 65
      i32.sub
      return
    end
    local.get 0
    i32.const 97
    i32.ge_s
    local.get 0
    i32.const 122
    i32.le_s
    i32.and
    if
      local.get 0
      i32.const 71
      i32.sub
      return
    end
    local.get 0
    i32.const 48
    i32.ge_s
    local.get 0
    i32.const 57
    i32.le_s
    i32.and
    if
      local.get 0
      i32.const 4
      i32.add
      return
    end
    local.get 0
    i32.const 43
    i32.eq
    if
      i32.const 62
      return
    end
    local.get 0
    i32.const 47
    i32.eq
    if
      i32.const 63
      return
    end
    local.get 0
    i32.const 61
    i32.eq
    if
      i32.const -2
      return
    end
    i32.const -1
  )
  (func $b64_decode (param i32) (param i32) (param i32) (result i32)
    (local i32 i32 i32 i32 i32)
    i32.const 0
    local.set 3
    i32.const 0
    local.set 4
    i32.const 0
    local.set 5
    i32.const 0
    local.set 6
    block
      loop
        local.get 3
        local.get 1
        i32.ge_s
        br_if 1
        local.get 0
        local.get 3
        i32.add
        i32.load8_u
        call $b64_val
        local.tee 7
        i32.const 0
        i32.ge_s
        if
          local.get 4
          i32.const 6
          i32.shl
          local.get 7
          i32.or
          local.set 4
          local.get 5
          i32.const 6
          i32.add
          local.tee 5
          i32.const 8
          i32.ge_s
          if
            local.get 5
            i32.const 8
            i32.sub
            local.set 5
            local.get 2
            local.get 6
            i32.add
            local.get 4
            local.get 5
            i32.shr_u
            i32.const 255
            i32.and
            i32.store8
            local.get 6
            i32.const 1
            i32.add
            local.set 6
          end
        end
        local.get 3
        i32.const 1
        i32.add
        local.set 3
        br 0
      end
    end
    local.get 6
  )
  (func $b64_encode (param i32) (param i32) (param i32) (result i32)
    (local i32 i32 i32 i32 i32 i32 i32 i32)
    i32.const 66993
    local.set 10
    i32.const 0
    local.set 3
    i32.const 0
    local.set 4
    block
      loop
        local.get 3
        local.get 1
        i32.ge_s
        br_if 1
        local.get 1
        local.get 3
        i32.sub
        local.set 5
        local.get 0
        local.get 3
        i32.add
        i32.load8_u
        local.set 6
        local.get 5
        i32.const 1
        i32.gt_s
        if (result i32)
          local.get 0
          local.get 3
          i32.add
          i32.const 1
          i32.add
          i32.load8_u
        else
          i32.const 0
        end
        local.set 7
        local.get 5
        i32.const 2
        i32.gt_s
        if (result i32)
          local.get 0
          local.get 3
          i32.add
          i32.const 2
          i32.add
          i32.load8_u
        else
          i32.const 0
        end
        local.set 8
        local.get 6
        i32.const 16
        i32.shl
        local.get 7
        i32.const 8
        i32.shl
        i32.or
        local.get 8
        i32.or
        local.set 9
        local.get 2
        local.get 4
        i32.add
        local.get 10
        local.get 9
        i32.const 18
        i32.shr_u
        i32.const 63
        i32.and
        i32.add
        i32.load8_u
        i32.store8
        local.get 2
        local.get 4
        i32.add
        i32.const 1
        i32.add
        local.get 10
        local.get 9
        i32.const 12
        i32.shr_u
        i32.const 63
        i32.and
        i32.add
        i32.load8_u
        i32.store8
        local.get 2
        local.get 4
        i32.add
        i32.const 2
        i32.add
        local.get 5
        i32.const 1
        i32.gt_s
        if (result i32)
          local.get 10
          local.get 9
          i32.const 6
          i32.shr_u
          i32.const 63
          i32.and
          i32.add
          i32.load8_u
        else
          i32.const 61
        end
        i32.store8
        local.get 2
        local.get 4
        i32.add
        i32.const 3
        i32.add
        local.get 5
        i32.const 2
        i32.gt_s
        if (result i32)
          local.get 10
          local.get 9
          i32.const 63
          i32.and
          i32.add
          i32.load8_u
        else
          i32.const 61
        end
        i32.store8
        local.get 4
        i32.const 4
        i32.add
        local.set 4
        local.get 3
        i32.const 3
        i32.add
        local.set 3
        br 0
      end
    end
    local.get 4
  )
  (func $write_u32_dec (param i32) (param i32) (result i32)
    (local i32 i32 i32 i32)
    local.get 0
    i32.const 0
    i32.le_s
    if
      local.get 1
      i32.const 48
      i32.store8
      local.get 1
      i32.const 1
      i32.add
      return
    end
    local.get 0
    local.set 2
    i32.const 0
    local.set 3
    i32.const 196736
    local.set 4
    block
      loop
        local.get 2
        i32.eqz
        br_if 1
        local.get 4
        local.get 3
        i32.add
        local.get 2
        i32.const 10
        i32.rem_u
        i32.const 48
        i32.add
        i32.store8
        local.get 2
        i32.const 10
        i32.div_u
        local.set 2
        local.get 3
        i32.const 1
        i32.add
        local.set 3
        br 0
      end
    end
    i32.const 0
    local.set 5
    block
      loop
        local.get 5
        local.get 3
        i32.ge_s
        br_if 1
        local.get 1
        local.get 5
        i32.add
        local.get 4
        local.get 3
        i32.add
        i32.const 1
        i32.sub
        local.get 5
        i32.sub
        i32.load8_u
        i32.store8
        local.get 5
        i32.const 1
        i32.add
        local.set 5
        br 0
      end
    end
    local.get 1
    local.get 3
    i32.add
  )
  (func $build_validation_probe (export "build_validation_probe") (param i32) (param i32) (result i64)
    i32.const 65536
    i32.const 274
    call $pack_slice
  )
  (func $verify_validation_result (export "verify_validation_result") (param i32) (param i32) (result i64)
    (local i32)
    local.get 0
    local.get 1
    i32.const 67538
    i32.const 14
    call $extract_json_int
    local.tee 2
    i32.const 200
    i32.ge_s
    local.get 2
    i32.const 300
    i32.lt_s
    i32.and
    if (result i64)
      i32.const 65810
      i32.const 51
      call $pack_slice
    else
      i32.const 65861
      i32.const 90
      call $pack_slice
    end
  )
  (func $transform_request (export "transform_request") (param i32) (param i32) (result i64)
    (local i32 i32 i32 i32)
    local.get 0
    local.get 1
    i32.const 67405
    i32.const 27
    i32.const 196608
    call $extract_json_str
    local.tee 2
    i32.const 0
    i32.lt_s
    if
      i32.const 0
      local.set 2
    end
    i32.const 196608
    local.get 2
    i32.const 131072
    call $b64_decode
    local.set 3
    i32.const 131072
    local.set 4
    local.get 4
    local.get 3
    i32.const 67588
    i32.const 13
    call $find_substr
    i32.const 0
    i32.ge_s
    local.get 4
    local.get 3
    i32.const 67601
    i32.const 16
    call $find_substr
    i32.const 0
    i32.lt_s
    i32.and
    local.get 3
    i32.const 1
    i32.gt_s
    i32.and
    if
      local.get 3
      i32.const 1
      i32.sub
      local.set 5
      block
        loop
          local.get 5
          i32.const 0
          i32.le_s
          br_if 1
          i32.const 131072
          local.get 5
          i32.add
          i32.load8_u
          i32.const 125
          i32.eq
          if
            i32.const 131072
            local.get 5
            i32.add
            i32.const 67364
            i32.const 41
            call $memcpy_ret
            i32.const 131072
            i32.sub
            local.set 3
            br 2
          end
          local.get 5
          i32.const 1
          i32.sub
          local.set 5
          br 0
        end
      end
    end
    i32.const 204800
    local.set 4
    local.get 4
    i32.const 67057
    i32.const 140
    call $memcpy_ret
    local.set 4
    i32.const 131072
    local.get 3
    local.get 4
    call $b64_encode
    local.get 4
    i32.add
    local.set 4
    local.get 4
    i32.const 67197
    i32.const 167
    call $memcpy_ret
    local.set 4
    i32.const 204800
    local.get 4
    i32.const 204800
    i32.sub
    call $pack_slice
  )
  (func $transform_response (export "transform_response") (param i32) (param i32) (result i64)
    (local i32 i32 i32 i32 i32 i32 i32)
    local.get 0
    local.get 1
    i32.const 67432
    i32.const 21
    i32.const 196608
    call $extract_json_str
    local.tee 2
    i32.const 0
    i32.lt_s
    if
      i32.const 0
      local.set 2
    end
    i32.const 196608
    local.get 2
    i32.const 131072
    call $b64_decode
    local.set 3
    i32.const 131072
    local.get 3
    i32.const 67552
    i32.const 16
    call $extract_json_int
    local.tee 5
    i32.const 0
    i32.lt_s
    if
      i32.const 0
      local.set 5
    end
    i32.const 131072
    local.get 3
    i32.const 67568
    i32.const 20
    call $extract_json_int
    local.tee 6
    i32.const 0
    i32.lt_s
    if
      i32.const 0
      local.set 6
    end
    i32.const 131072
    local.get 3
    i32.const 67617
    i32.const 9
    i32.const 196864
    call $extract_json_str
    local.tee 7
    i32.const 0
    i32.lt_s
    if
      i32.const 0
      local.set 7
    end
    i32.const 131072
    local.get 3
    i32.const 67626
    i32.const 17
    i32.const 197120
    call $extract_json_str
    local.tee 8
    i32.const 0
    i32.lt_s
    if
      i32.const 0
      local.set 8
    end
    i32.const 204800
    local.set 4
    local.get 4
    i32.const 67676
    i32.const 29
    call $memcpy_ret
    local.set 4
    i32.const 131072
    local.get 3
    local.get 4
    call $b64_encode
    local.get 4
    i32.add
    local.set 4
    local.get 4
    i32.const 67705
    i32.const 26
    call $memcpy_ret
    local.set 4
    local.get 5
    local.get 4
    call $write_u32_dec
    local.set 4
    local.get 4
    i32.const 67731
    i32.const 17
    call $memcpy_ret
    local.set 4
    local.get 6
    local.get 4
    call $write_u32_dec
    local.set 4
    local.get 4
    i32.const 67748
    i32.const 57
    call $memcpy_ret
    local.set 4
    local.get 4
    i32.const 196864
    local.get 7
    call $memcpy_ret
    local.set 4
    local.get 4
    i32.const 67805
    i32.const 19
    call $memcpy_ret
    local.set 4
    local.get 4
    i32.const 197120
    local.get 8
    call $memcpy_ret
    local.set 4
    local.get 4
    i32.const 67824
    i32.const 2
    call $memcpy_ret
    local.set 4
    i32.const 204800
    local.get 4
    i32.const 204800
    i32.sub
    call $pack_slice
  )
  (func $transform_stream_chunk (export "transform_stream_chunk") (param i32) (param i32) (result i64)
    (local i32 i32 i32 i32 i32 i32 i32 i32 i32 i32 i32 i32 i32 i32 i32 i32 i32 i32)
    i32.const 0
    local.set 3
    local.get 0
    local.get 1
    i32.const 67453
    i32.const 17
    i32.const 196608
    call $extract_json_str
    local.tee 2
    i32.const 0
    i32.gt_s
    if
      i32.const 196608
      local.get 2
      i32.const 147456
      call $b64_decode
      local.set 3
    end
    i32.const 0
    local.set 4
    local.get 0
    local.get 1
    i32.const 67470
    i32.const 22
    i32.const 196608
    call $extract_json_str
    local.tee 2
    i32.const 0
    i32.gt_s
    if
      i32.const 196608
      local.get 2
      i32.const 147456
      local.get 3
      i32.add
      call $b64_decode
      local.set 4
    end
    local.get 3
    local.get 4
    i32.add
    local.set 5
    local.get 0
    local.get 1
    i32.const 67525
    i32.const 13
    call $find_substr
    i32.const 0
    i32.ge_s
    local.set 6
    i32.const 0
    local.set 7
    i32.const 180224
    local.set 11
    i32.const 0
    local.set 12
    local.get 6
    local.set 13
    i32.const 0
    local.set 14
    i32.const 0
    local.set 15
    i32.const 0
    local.set 16
    block
      loop
        local.get 7
        local.get 5
        i32.ge_s
        br_if 1
        local.get 7
        local.set 8
        block
          loop
            local.get 8
            local.get 5
            i32.ge_s
            br_if 1
            i32.const 147456
            local.get 8
            i32.add
            i32.load8_u
            i32.const 10
            i32.eq
            br_if 1
            local.get 8
            i32.const 1
            i32.add
            local.set 8
            br 0
          end
        end
        local.get 8
        local.get 5
        i32.ge_s
        if
          local.get 6
          i32.eqz
          if
            br 3
          end
          local.get 7
          local.set 9
          local.get 5
          local.set 10
          local.get 5
          local.set 7
        else
          local.get 7
          local.set 9
          local.get 8
          local.set 10
          local.get 8
          i32.const 1
          i32.add
          local.set 7
        end
        block
          loop
            local.get 9
            local.get 10
            i32.ge_s
            br_if 1
            i32.const 147456
            local.get 9
            i32.add
            i32.load8_u
            i32.const 32
            i32.gt_s
            br_if 1
            local.get 9
            i32.const 1
            i32.add
            local.set 9
            br 0
          end
        end
        block
          loop
            local.get 10
            local.get 9
            i32.le_s
            br_if 1
            i32.const 147456
            local.get 10
            i32.const 1
            i32.sub
            i32.add
            i32.load8_u
            i32.const 32
            i32.gt_s
            br_if 1
            local.get 10
            i32.const 1
            i32.sub
            local.set 10
            br 0
          end
        end
        local.get 9
        local.get 10
        i32.ge_s
        br_if 0
        i32.const 147456
        local.get 9
        i32.add
        i32.load8_u
        i32.const 58
        i32.eq
        br_if 0
        local.get 10
        local.get 9
        i32.sub
        i32.const 5
        i32.lt_s
        br_if 0
        i32.const 147456
        local.get 9
        i32.add
        i32.const 5
        i32.const 67649
        i32.const 5
        call $find_substr
        i32.const 0
        i32.ne
        br_if 0
        local.get 9
        i32.const 5
        i32.add
        local.set 9
        block
          loop
            local.get 9
            local.get 10
            i32.ge_s
            br_if 1
            i32.const 147456
            local.get 9
            i32.add
            i32.load8_u
            i32.const 32
            i32.gt_s
            br_if 1
            local.get 9
            i32.const 1
            i32.add
            local.set 9
            br 0
          end
        end
        local.get 9
        local.get 10
        i32.ge_s
        br_if 0
        local.get 12
        i32.const 0
        i32.gt_s
        if
          local.get 11
          i32.const 44
          i32.store8
          local.get 11
          i32.const 1
          i32.add
          local.set 11
        end
        local.get 12
        i32.const 1
        i32.add
        local.set 12
        local.get 11
        i32.const 34
        i32.store8
        local.get 11
        i32.const 1
        i32.add
        local.set 11
        local.get 10
        local.get 9
        i32.sub
        local.set 18
        local.get 18
        i32.const 6
        i32.eq
        i32.const 147456
        local.get 9
        i32.add
        i32.const 6
        i32.const 67643
        i32.const 6
        call $find_substr
        i32.const 0
        i32.eq
        i32.and
        if
          i32.const 1
          local.set 13
          i32.const 67662
          i32.const 14
          local.get 11
          call $b64_encode
          local.get 11
          i32.add
          local.set 11
        else
          i32.const 163840
          local.set 17
          local.get 17
          i32.const 67654
          i32.const 6
          call $memcpy_ret
          local.set 17
          local.get 17
          i32.const 147456
          local.get 9
          i32.add
          local.get 18
          call $memcpy_ret
          local.set 17
          local.get 17
          i32.const 67660
          i32.const 2
          call $memcpy_ret
          local.set 17
          i32.const 163840
          local.get 17
          i32.const 163840
          i32.sub
          local.get 11
          call $b64_encode
          local.get 11
          i32.add
          local.set 11
          i32.const 147456
          local.get 9
          i32.add
          local.get 18
          i32.const 67552
          i32.const 16
          call $extract_json_int
          local.tee 19
          i32.const 0
          i32.ge_s
          if
            i32.const 1
            local.set 14
            local.get 19
            local.set 15
          end
          i32.const 147456
          local.get 9
          i32.add
          local.get 18
          i32.const 67568
          i32.const 20
          call $extract_json_int
          local.tee 19
          i32.const 0
          i32.ge_s
          if
            i32.const 1
            local.set 14
            local.get 19
            local.set 16
          end
        end
        local.get 11
        i32.const 34
        i32.store8
        local.get 11
        i32.const 1
        i32.add
        local.set 11
        br 0
      end
    end
    i32.const 204800
    local.set 17
    local.get 17
    i32.const 67826
    i32.const 23
    call $memcpy_ret
    local.set 17
    local.get 7
    local.get 5
    i32.lt_s
    local.get 6
    i32.eqz
    i32.and
    if
      i32.const 147456
      local.get 7
      i32.add
      local.get 5
      local.get 7
      i32.sub
      local.get 17
      call $b64_encode
      local.get 17
      i32.add
      local.set 17
    end
    local.get 17
    i32.const 67849
    i32.const 27
    call $memcpy_ret
    local.set 17
    local.get 17
    i32.const 180224
    local.get 11
    i32.const 180224
    i32.sub
    call $memcpy_ret
    local.set 17
    local.get 14
    if
      local.get 17
      i32.const 67876
      i32.const 38
      call $memcpy_ret
      local.set 17
      local.get 15
      local.get 17
      call $write_u32_dec
      local.set 17
      local.get 17
      i32.const 67951
      i32.const 17
      call $memcpy_ret
      local.set 17
      local.get 16
      local.get 17
      call $write_u32_dec
      local.set 17
      local.get 17
      i32.const 67968
      i32.const 35
      call $memcpy_ret
      local.set 17
      local.get 17
      i32.const 67915
      i32.const 20
      call $memcpy_ret
      local.set 17
    else
      local.get 17
      i32.const 67914
      i32.const 21
      call $memcpy_ret
      local.set 17
    end
    local.get 13
    if
      local.get 17
      i32.const 68003
      i32.const 5
      call $memcpy_ret
      local.set 17
    else
      local.get 17
      i32.const 68008
      i32.const 6
      call $memcpy_ret
      local.set 17
    end
    i32.const 204800
    local.get 17
    i32.const 204800
    i32.sub
    call $pack_slice
  )
  (func $normalize_error (export "normalize_error") (param i32) (param i32) (result i64)
    (local i32)
    local.get 0
    local.get 1
    i32.const 67538
    i32.const 14
    call $extract_json_int
    local.tee 2
    i32.const 401
    i32.eq
    local.get 2
    i32.const 403
    i32.eq
    i32.or
    if
      i32.const 65951
      i32.const 199
      call $pack_slice
      return
    end
    local.get 2
    i32.const 429
    i32.eq
    if
      i32.const 66150
      i32.const 213
      call $pack_slice
      return
    end
    local.get 2
    i32.const 500
    i32.ge_s
    if
      i32.const 66363
      i32.const 208
      call $pack_slice
      return
    end
    i32.const 66571
    i32.const 196
    call $pack_slice
  )
  (func $extract_usage (export "extract_usage") (param i32) (param i32) (result i64)
    (local i32 i32 i32 i32 i32)
    local.get 0
    local.get 1
    i32.const 67492
    i32.const 33
    i32.const 196608
    call $extract_json_str
    local.tee 2
    i32.const 0
    i32.lt_s
    if
      i32.const 0
      local.set 2
    end
    i32.const 196608
    local.get 2
    i32.const 131072
    call $b64_decode
    local.set 3
    i32.const 131072
    local.get 3
    i32.const 67552
    i32.const 16
    call $extract_json_int
    local.tee 4
    i32.const 0
    i32.lt_s
    if
      i32.const 0
      local.set 4
    end
    i32.const 131072
    local.get 3
    i32.const 67568
    i32.const 20
    call $extract_json_int
    local.tee 5
    i32.const 0
    i32.lt_s
    if
      i32.const 0
      local.set 5
    end
    i32.const 204800
    local.set 6
    local.get 6
    i32.const 67935
    i32.const 16
    call $memcpy_ret
    local.set 6
    local.get 4
    local.get 6
    call $write_u32_dec
    local.set 6
    local.get 6
    i32.const 67951
    i32.const 17
    call $memcpy_ret
    local.set 6
    local.get 5
    local.get 6
    call $write_u32_dec
    local.set 6
    local.get 6
    i32.const 67968
    i32.const 35
    call $memcpy_ret
    local.set 6
    i32.const 204800
    local.get 6
    i32.const 204800
    i32.sub
    call $pack_slice
  )
  (func $inspect_native_request (export "inspect_native_request") (param i32) (param i32) (result i64)
    i32.const 66767
    i32.const 210
    call $pack_slice
  )
  (func $evaluate_guardrail (export "evaluate_guardrail") (param i32) (param i32) (result i64)
    i32.const 66977
    i32.const 16
    call $pack_slice
  )
  (func $transform (export "transform") (param i32) (param i32) (result i64)
    local.get 0
    local.get 1
    call $transform_request
  )

  (data (i32.const 65536) "{\22method\22:\22GET\22,\22target_url\22:\22https://api.openai.com/v1/models\22,\22safe_headers\22:{\22Accept\22:\22application/json\22},\22credential_injections\22:[{\22secret_slot_name\22:\22api_key\22,\22target_location\22:\22CREDENTIAL_TARGET_LOCATION_HEADER\22,\22field_name\22:\22Authorization\22,\22value_prefix\22:\22Bearer \22}]}{\22valid\22:true,\22detected_model_ids\22:[\22gpt-4o-mini\22]}{\22valid\22:false,\22failure_code\22:\22auth_failed\22,\22sanitized_message\22:\22upstream probe rejected\22}{\22canonical_code\22:\22upstream_auth_failed\22,\22error_type\22:\22authentication_error\22,\22retryable_pre_stream\22:false,\22trigger_connection_reauth\22:true,\22sanitized_public_message\22:\22upstream authentication failed\22}{\22canonical_code\22:\22upstream_rate_limited\22,\22error_type\22:\22rate_limit_error\22,\22retryable_pre_stream\22:true,\22cooldown_ms\22:5000,\22trigger_connection_reauth\22:false,\22sanitized_public_message\22:\22upstream rate limit exceeded\22}{\22canonical_code\22:\22upstream_unavailable\22,\22error_type\22:\22server_error\22,\22retryable_pre_stream\22:true,\22cooldown_ms\22:2000,\22trigger_connection_reauth\22:false,\22sanitized_public_message\22:\22upstream service unavailable\22}{\22canonical_code\22:\22upstream_bad_request\22,\22error_type\22:\22invalid_request_error\22,\22retryable_pre_stream\22:false,\22trigger_connection_reauth\22:false,\22sanitized_public_message\22:\22upstream request rejected\22}{\22target_model\22:\22gpt-4o-mini\22,\22stream\22:false,\22credential_injections\22:[{\22secret_slot_name\22:\22api_key\22,\22target_location\22:\22CREDENTIAL_TARGET_LOCATION_HEADER\22,\22field_name\22:\22Authorization\22,\22value_prefix\22:\22Bearer \22}]}{\22allowed\22:true}ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/{\22method\22:\22POST\22,\22target_url\22:\22https://api.openai.com/v1/chat/completions\22,\22safe_headers\22:{\22Content-Type\22:\22application/json\22},\22body_bytes\22:\22\22,\22credential_injections\22:[{\22secret_slot_name\22:\22api_key\22,\22target_location\22:\22CREDENTIAL_TARGET_LOCATION_HEADER\22,\22field_name\22:\22Authorization\22,\22value_prefix\22:\22Bearer \22}]},\22stream_options\22:{\22include_usage\22:true}}\22normalized_request_json\22:\22\22raw_response_body\22:\22\22session_state\22:\22\22raw_upstream_chunk\22:\22\22raw_response_or_trailer_bytes\22:\22\22is_eof\22:true\22http_status\22:\22prompt_tokens\22:\22completion_tokens\22:\22stream\22:true\22stream_options\22\22model\22:\22\22finish_reason\22:\22[DONE]data:data: \0a\0adata: [DONE]\0a\0a{\22normalized_response_json\22:\22\22,\22usage\22:{\22input_tokens\22:,\22output_tokens\22:,\22accuracy\22:\22USAGE_ACCURACY_EXACT\22},\22upstream_model_id\22:\22\22,\22finish_reason\22:\22\22}{\22next_session_state\22:\22\22,\22normalized_sse_frames\22:[],\22incremental_usage\22:{\22input_tokens\22:],\22stream_completed\22:{\22input_tokens\22:,\22output_tokens\22:,\22accuracy\22:\22USAGE_ACCURACY_EXACT\22}true}false}")
)
