use snos::SnOsRunner; 
use snos::SharedState; 
use snos::TransactionExecutionInfo; 

use blockifier::state::cached_state::CachedState;
use blockifier::block_context::BlockContext;

use crate::juno_state_reader::{ptr_to_felt, JunoStateReader};
use starknet_api::{
    deprecated_contract_class::EntryPointType,
    hash::StarkFelt,
    transaction::Fee,
};

#[repr(C)]
#[derive(Clone, Copy)]
pub struct BlockInfo {
    pub block_number: c_ulonglong,
    pub block_timestamp: c_ulonglong,
    pub sequencer_address: [c_uchar; 32],
    pub gas_price_wei: [c_uchar; 32],
    pub gas_price_fri: [c_uchar; 32],
    pub version: *const c_char,
    pub block_hash_to_be_revealed: [c_uchar; 32],
    pub data_gas_price_wei: [c_uchar; 32],
    pub data_gas_price_fri: [c_uchar; 32],
    pub use_blob_data: c_uchar,
}


#[no_mangle]
pub extern "C" fn snosRunnerRun(
    block_info_ptr: *const BlockInfo,
    reader_handle: usize,
    chain_id: *const c_char,
) {
    let block_info = unsafe { *block_info_ptr };
    let reader = JunoStateReader::new(reader_handle, block_info.block_number); 
    let mut cached_state = CachedState::new(reader, GlobalContractCache::new(1));
    let chain_id_str = unsafe { CStr::from_ptr(chain_id) }.to_str().unwrap();
    let block_context: BlockContext = build_block_context(&mut cached_state, &block_info, chain_id_str); 
    let shared_state = SharedState::new(cached_state, block_context); 
    
    let execution_infos = vec![TransactionExecutionInfo::new()]; // Todo: ???

    // SNOS runner
    // Todo: override input path and block context
    let runner = SnOsRunner::default();
    let runner = runner.with_input_path("new/input/path"); // Todo: pass in the actual json data
    let runner = runner.with_block_context(block_context);

    // Execute the run method
    match runner.run(shared_state, execution_infos) {
        Ok(result_pie) => println!("Success: {:?}", result_pie),
        Err(e) => eprintln!("Error: {:?}", e),
    }
}


fn build_block_context(
    state: &mut dyn State,
    block_info: &BlockInfo,
    chain_id_str: &str,
) -> BlockContext {
    let sequencer_addr =  StarkFelt::new(block_info.sequencer_address).unwrap();
    let gas_price_wei_felt = StarkFelt::new(block_info.gas_price_wei).unwrap();
    let gas_price_fri_felt = StarkFelt::new(block_info.gas_price_fri).unwrap();
    let data_gas_price_wei_felt = StarkFelt::new(block_info.data_gas_price_wei).unwrap();
    let data_gas_price_fri_felt = StarkFelt::new(block_info.data_gas_price_fri).unwrap();
    let default_gas_price = NonZeroU128::new(1).unwrap();

    let mut old_block_number_and_hash: Option<BlockNumberHashPair> = None;
    if block_info.block_number >= 10 {
        old_block_number_and_hash = Some(BlockNumberHashPair{
            number: starknet_api::block::BlockNumber(block_info.block_number - 10),
            hash: BlockHash(StarkFelt::new(block_info.block_hash_to_be_revealed).unwrap()),
        })
    }
    pre_process_block(state, old_block_number_and_hash, BlockifierBlockInfo{
        block_number: starknet_api::block::BlockNumber(block_info.block_number),
        block_timestamp: starknet_api::block::BlockTimestamp(block_info.block_timestamp),
        sequencer_address: ContractAddress(PatriciaKey::try_from(sequencer_addr).unwrap()),
        gas_prices: GasPrices {
            eth_l1_gas_price: NonZeroU128::new(felt_to_u128(gas_price_wei_felt)).unwrap_or(default_gas_price),
            strk_l1_gas_price: NonZeroU128::new(felt_to_u128(gas_price_fri_felt)).unwrap_or(default_gas_price),
            eth_l1_data_gas_price: NonZeroU128::new(felt_to_u128(data_gas_price_wei_felt)).unwrap_or(default_gas_price),
            strk_l1_data_gas_price: NonZeroU128::new(felt_to_u128(data_gas_price_fri_felt)).unwrap_or(default_gas_price),
        },
        use_kzg_da: block_info.use_blob_data == 1,
    }, ChainInfo{
        chain_id: ChainId(chain_id_str.to_string()),
        fee_token_addresses: FeeTokenAddresses {
            // both addresses are the same for all networks
            eth_fee_token_address: ContractAddress::try_from(StarkHash::try_from("0x049d36570d4e46f48e99674bd3fcc84644ddd6b96f7c741b1562b82f9e004dc7").unwrap()).unwrap(),
            strk_fee_token_address: ContractAddress::try_from(StarkHash::try_from("0x04718f5a0fc34cc1af16a1cdee98ffb20c31f5cd61d6ab07201858f4287c938d").unwrap()).unwrap(),
        },
    }, get_versioned_constants(block_info.version)).unwrap()
}