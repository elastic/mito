use std::ffi::{CStr, CString};
use std::mem;
use std::os::raw::{c_char, c_void};

#[no_mangle]
pub extern "C" fn add_one(x: i64) -> i64 {
    x + 1
}

#[no_mangle]
pub extern "C" fn sum(x: i64, y: i64) -> i64 {
    x + y
}

#[no_mangle]
pub extern "C" fn allocate(size: usize) -> *mut c_void {
    let mut buffer = Vec::with_capacity(size);
    let pointer = buffer.as_mut_ptr();
    mem::forget(buffer);

    pointer as *mut c_void
}

#[no_mangle]
pub extern "C" fn deallocate(pointer: *mut c_void, capacity: usize) {
    unsafe {
        let _ = Vec::from_raw_parts(pointer, 0, capacity);
    }
}

#[no_mangle]
pub extern "C" fn concat(a: *mut c_char, b: *mut c_char) -> *mut c_char {
    let a = unsafe { CStr::from_ptr(a).to_bytes().to_vec() };
    let b = unsafe { CStr::from_ptr(b).to_bytes().to_vec() };
    unsafe { CString::from_vec_unchecked([a, b].concat().to_vec()) }.into_raw()
}
