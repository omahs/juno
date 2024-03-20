use snos::SnOsRunner; 
use snos::SharedState; 
use snos::TransactionExecutionInfo; 

use blockifier::state::cached_state::CachedState;
use blockifier::block_context::BlockContext;

use crate::juno_state_reader::{ptr_to_felt, JunoStateReader};


#[no_mangle]
pub extern "C" fn snosRunnerRun(
    block_info_ptr: *const BlockInfo,
    reader_handle: usize,
    chain_id: *const c_char,
) {
    
    let reader = JunoStateReader::new(reader_handle, block_info.block_number); 
    let mut cached_state = CachedState::new(reader, GlobalContractCache::new(1));
    let chain_id_str = unsafe { CStr::from_ptr(chain_id) }.to_str().unwrap();
    let block_context: BlockContext = build_block_context(&mut cached_state, &block_info, chain_id_str); 
    let shared_state = SharedState::new(cached_state, block_context); 
    
    let execution_infos = vec![TransactionExecutionInfo::new()]; // Todo: ???

    // SNOS runner
    // Todo: override input path and block context
    let runner = SnOsRunner::default();
    

    // Execute the run method
    match runner.run(shared_state, execution_infos) {
        Ok(result_pie) => println!("Success: {:?}", result_pie),
        Err(e) => eprintln!("Error: {:?}", e),
    }
}