import {render,screen,fireEvent,cleanup} from '@testing-library/react'
import {MemoryRouter} from 'react-router-dom'
import {afterEach,expect,it,vi} from 'vitest'
import {Souls} from './Souls'
const mocks=vi.hoisted(()=>({soul:{id:'s1',name:'API',type:'http',target:'https://example.com',enabled:true,status:'healthy',latency:0 as number|undefined},fetch:vi.fn()}))
vi.mock('../stores/soulStore',()=>({useSoulStore:()=>({souls:[mocks.soul],initialChecks:{},fetchSouls:mocks.fetch,createSoul:vi.fn(),retryInitialCheck:vi.fn(),updateSoul:vi.fn(),deleteSoul:vi.fn()})}))
vi.mock('../hooks/useRealtimeRefresh',()=>({useRealtimeRefresh:()=>null}))
afterEach(cleanup)
const mount=()=>render(<MemoryRouter><Souls/></MemoryRouter>)
it.each([['list',0],['grid',0],['list',25],['grid',25],['list',undefined],['grid',undefined]])('latency %s %s',(mode,latency)=>{
 mocks.soul.latency=latency as number|undefined;mount();if(mode==='grid')fireEvent.click(screen.getByLabelText('Grid view'))
 if(latency===undefined)expect(screen.queryByText(/^[0-9]+ms$/)).not.toBeInTheDocument();else expect(screen.getByText(`${latency}ms`)).toBeInTheDocument()
})
