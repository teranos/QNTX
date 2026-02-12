//! Low-level IndexedDB helpers using web-sys
//!
//! Wraps the callback-based IndexedDB API into Rust futures using
//! `wasm_bindgen_futures::JsFuture` and `js_sys::Promise`.

use js_sys::Promise;
use std::cell::RefCell;
use std::rc::Rc;
use wasm_bindgen::prelude::*;
use wasm_bindgen::JsCast;
use web_sys::{
    IdbDatabase, IdbFactory, IdbObjectStore, IdbOpenDbRequest, IdbRequest, IdbTransaction,
    IdbTransactionMode,
};

use crate::error::{IndexedDbError, Result};

const DB_VERSION: u32 = 1;

/// Object store name for attestations.
pub const STORE_NAME: &str = "attestations";

/// Type alias for upgrade closure to reduce complexity
type UpgradeClosure = Rc<RefCell<Option<Closure<dyn FnMut(web_sys::IdbVersionChangeEvent)>>>>;

/// Get the global IndexedDB factory.
pub fn idb_factory() -> Result<IdbFactory> {
    let global = js_sys::global();

    let idb: JsValue = js_sys::Reflect::get(&global, &"indexedDB".into())
        .map_err(|_| IndexedDbError::NotAvailable("no indexedDB on global".into()))?;

    if idb.is_undefined() || idb.is_null() {
        return Err(IndexedDbError::NotAvailable(
            "indexedDB is null/undefined".into(),
        ));
    }

    idb.dyn_into::<IdbFactory>()
        .map_err(|_| IndexedDbError::NotAvailable("indexedDB is not IdbFactory".into()))
}

/// Convert an IdbRequest into a JS Promise that resolves with the request's result.
fn request_to_promise(req: &IdbRequest) -> Promise {
    let req_success = req.clone();
    let req_error = req.clone();

    let promise = Promise::new(&mut move |resolve, reject| {
        // Store closures in Rc<RefCell> to manage their lifetime without leaking
        type ClosurePair = (
            Closure<dyn FnMut(web_sys::Event)>,
            Closure<dyn FnMut(web_sys::Event)>,
        );
        let closures: Rc<RefCell<Option<ClosurePair>>> = Rc::new(RefCell::new(None));

        let req_s = req_success.clone();
        let closures_for_success = closures.clone();
        let on_success = Closure::wrap(Box::new(move |_event: web_sys::Event| {
            let result = req_s.result().unwrap_or(JsValue::UNDEFINED);
            let _ = resolve.call1(&JsValue::UNDEFINED, &result);
            // Clean up both closures after success
            *closures_for_success.borrow_mut() = None;
        }) as Box<dyn FnMut(web_sys::Event)>);

        let req_e = req_error.clone();
        let closures_for_error = closures.clone();
        let on_error = Closure::wrap(Box::new(move |_event: web_sys::Event| {
            let msg = req_e
                .error()
                .map(|opt| {
                    opt.map(|e| JsValue::from(e.message()))
                        .unwrap_or_else(|| JsValue::from_str("unknown IDB error"))
                })
                .unwrap_or_else(|_| JsValue::from_str("unknown IDB error"));
            let _ = reject.call1(&JsValue::UNDEFINED, &msg);
            // Clean up both closures after error
            *closures_for_error.borrow_mut() = None;
        }) as Box<dyn FnMut(web_sys::Event)>);

        req_success.set_onsuccess(Some(on_success.as_ref().unchecked_ref()));
        req_error.set_onerror(Some(on_error.as_ref().unchecked_ref()));

        // Store both closures to keep them alive until one fires
        *closures.borrow_mut() = Some((on_success, on_error));
    });

    promise
}

/// Convert an IdbTransaction completion into a JS Promise.
fn transaction_to_promise(tx: &IdbTransaction) -> Promise {
    let tx_complete = tx.clone();
    let tx_error = tx.clone();

    let promise = Promise::new(&mut move |resolve, reject| {
        // Store closures in Rc<RefCell> to manage their lifetime without leaking
        type ClosurePair = (
            Closure<dyn FnMut(web_sys::Event)>,
            Closure<dyn FnMut(web_sys::Event)>,
        );
        let closures: Rc<RefCell<Option<ClosurePair>>> = Rc::new(RefCell::new(None));

        let closures_for_complete = closures.clone();
        let on_complete = Closure::wrap(Box::new(move |_event: web_sys::Event| {
            let _ = resolve.call0(&JsValue::UNDEFINED);
            // Clean up both closures after completion
            *closures_for_complete.borrow_mut() = None;
        }) as Box<dyn FnMut(web_sys::Event)>);

        let tx_e = tx_error.clone();
        let closures_for_error = closures.clone();
        let on_error = Closure::wrap(Box::new(move |_event: web_sys::Event| {
            let msg = tx_e
                .error()
                .map(|e| JsValue::from(e.message()))
                .unwrap_or_else(|| JsValue::from_str("transaction error"));
            let _ = reject.call1(&JsValue::UNDEFINED, &msg);
            // Clean up both closures after error
            *closures_for_error.borrow_mut() = None;
        }) as Box<dyn FnMut(web_sys::Event)>);

        tx_complete.set_oncomplete(Some(on_complete.as_ref().unchecked_ref()));
        tx_error.set_onerror(Some(on_error.as_ref().unchecked_ref()));

        // Store both closures to keep them alive until one fires
        *closures.borrow_mut() = Some((on_complete, on_error));
    });

    promise
}

/// Open (or create) the QNTX IndexedDB database.
pub async fn open_database(db_name: &str) -> Result<IdbDatabase> {
    let factory = idb_factory()?;

    let open_req: IdbOpenDbRequest = factory
        .open_with_u32(db_name, DB_VERSION)
        .map_err(|e| IndexedDbError::Open(format!("{:?}", e)))?;

    // Store upgrade closure to manage its lifetime without leaking
    let upgrade_closure: UpgradeClosure = Rc::new(RefCell::new(None));
    let upgrade_closure_for_drop = upgrade_closure.clone();

    // Handle upgradeneeded: create object store and indexes
    let on_upgrade = Closure::wrap(Box::new(move |event: web_sys::IdbVersionChangeEvent| {
        let target = event.target().expect("upgrade event has target");
        let req: IdbOpenDbRequest = target.unchecked_into();
        let db: IdbDatabase = req.result().expect("result on upgrade").unchecked_into();

        let store_name = String::from(STORE_NAME);
        if !db.object_store_names().contains(&store_name) {
            let params = web_sys::IdbObjectStoreParameters::new();
            js_sys::Reflect::set(&params, &"keyPath".into(), &"id".into()).expect("set keyPath");

            let store = db
                .create_object_store_with_optional_parameters(STORE_NAME, &params)
                .expect("create object store");

            // Multi-entry indexes for array fields (subjects, predicates, contexts, actors)
            let multi_params = web_sys::IdbIndexParameters::new();
            js_sys::Reflect::set(&multi_params, &"multiEntry".into(), &JsValue::TRUE)
                .expect("set multiEntry");

            store
                .create_index_with_str_and_optional_parameters(
                    "subjects",
                    "subjects",
                    &multi_params,
                )
                .expect("create subjects index");
            store
                .create_index_with_str_and_optional_parameters(
                    "predicates",
                    "predicates",
                    &multi_params,
                )
                .expect("create predicates index");
            store
                .create_index_with_str_and_optional_parameters(
                    "contexts",
                    "contexts",
                    &multi_params,
                )
                .expect("create contexts index");
            store
                .create_index_with_str_and_optional_parameters("actors", "actors", &multi_params)
                .expect("create actors index");

            // Simple indexes for ordering
            let simple_params = web_sys::IdbIndexParameters::new();
            store
                .create_index_with_str_and_optional_parameters(
                    "timestamp",
                    "timestamp",
                    &simple_params,
                )
                .expect("create timestamp index");
            store
                .create_index_with_str_and_optional_parameters(
                    "created_at",
                    "created_at",
                    &simple_params,
                )
                .expect("create created_at index");
        }
    }) as Box<dyn FnMut(web_sys::IdbVersionChangeEvent)>);

    open_req.set_onupgradeneeded(Some(on_upgrade.as_ref().unchecked_ref()));

    // Store closure to keep it alive during the open request
    *upgrade_closure.borrow_mut() = Some(on_upgrade);

    // Await the open request via promise
    let open_promise = request_to_promise(open_req.unchecked_ref());
    let result = wasm_bindgen_futures::JsFuture::from(open_promise)
        .await
        .map_err(|e| IndexedDbError::Open(format!("{:?}", e)))?;

    // Clean up upgrade closure now that open is complete
    *upgrade_closure_for_drop.borrow_mut() = None;

    result
        .dyn_into::<IdbDatabase>()
        .map_err(|_| IndexedDbError::Open("result is not IdbDatabase".into()))
}

/// Start a transaction on the attestations store.
pub fn begin_transaction(
    db: &IdbDatabase,
    mode: IdbTransactionMode,
) -> Result<(IdbTransaction, IdbObjectStore)> {
    let tx = db
        .transaction_with_str_and_mode(STORE_NAME, mode)
        .map_err(|e| IndexedDbError::Transaction(format!("{:?}", e)))?;
    let store = tx
        .object_store(STORE_NAME)
        .map_err(|e| IndexedDbError::Request(format!("{:?}", e)))?;
    Ok((tx, store))
}

/// Await an IdbRequest, resolving to its result JsValue.
pub async fn await_request(req: &IdbRequest) -> Result<JsValue> {
    let promise = request_to_promise(req);
    wasm_bindgen_futures::JsFuture::from(promise)
        .await
        .map_err(|e| IndexedDbError::Request(format!("{:?}", e)))
}

/// Await an IdbTransaction to complete.
pub async fn await_transaction(tx: &IdbTransaction) -> Result<()> {
    let promise = transaction_to_promise(tx);
    wasm_bindgen_futures::JsFuture::from(promise)
        .await
        .map_err(|e| IndexedDbError::Transaction(format!("{:?}", e)))?;
    Ok(())
}

/// Delete an IndexedDB database by name.
pub async fn delete_database(db_name: &str) -> Result<()> {
    let factory = idb_factory()?;
    let req = factory
        .delete_database(db_name)
        .map_err(|e| IndexedDbError::Open(format!("delete db: {:?}", e)))?;
    let promise = request_to_promise(req.unchecked_ref());
    wasm_bindgen_futures::JsFuture::from(promise)
        .await
        .map_err(|e| IndexedDbError::Open(format!("delete db: {:?}", e)))?;
    Ok(())
}
