use snos::SnOsRunner; 
use snos::SharedState; 
use snos::TransactionExecutionInfo; 

use blockifier::state::cached_state::CachedState;
use blockifier::block_context::BlockContext;

fn generate_pie(
    block_info_ptr: *const BlockInfo,
    reader_handle: usize,
    chain_id: *const c_char,
) {
    // SNOS runner
    let runner = SnOsRunner::default();

    // Pass in Juno state somehow
    let reader = JunoStateReader::new(reader_handle, block_info.block_number); // Todo: function inputs
    let mut cached_state = CachedState::new(reader, GlobalContractCache::new(1));
    let block_context: BlockContext = build_block_context(&mut cached_state, &block_info, chain_id_str); // Todo: function inputs + steal from vm

    let shared_state = SharedState::new(cached_state, block_context); 
    
    let execution_infos = vec![TransactionExecutionInfo::new()]; // Todo: ???

    // Execute the run method
    match runner.run(shared_state, execution_infos) {
        Ok(result_pie) => println!("Success: {:?}", result_pie),
        Err(e) => eprintln!("Error: {:?}", e),
    }
}