import {render,screen,cleanup} from '@testing-library/react'
import {MemoryRouter} from 'react-router-dom'
import {afterEach,expect,it,vi} from 'vitest'
import {Dashboard} from './Dashboard'
const mocks=vi.hoisted(()=>({stats:undefined as undefined|{souls:{total:number,healthy:number,degraded:number}}}))
vi.mock('../api/hooks',()=>({useSouls:()=>({souls:[],refetch:vi.fn()}),useStats:()=>({data:mocks.stats,refetch:vi.fn()}),useClusterStatus:()=>({data:undefined}),useJudgments:()=>({data:[]} )}))
vi.mock('../components/EventsFeed',()=>({EventsFeed:()=>null}))
vi.mock('recharts',()=>{const empty=()=>null;return {AreaChart:empty,Area:empty,XAxis:empty,YAxis:empty,CartesianGrid:empty,Tooltip:empty,ResponsiveContainer:empty,BarChart:empty,Bar:empty}})
afterEach(()=>{cleanup();mocks.stats=undefined})
const mount=()=>render(<MemoryRouter><Dashboard/></MemoryRouter>)
const balance=()=>screen.getByText('Balance').parentElement!
it.each([[undefined,'N/A'],[{souls:{total:0,healthy:0,degraded:0}},'N/A'],[{souls:{total:4,healthy:2,degraded:0}},'50%'],[{souls:{total:2,healthy:0,degraded:0}},'0%'],[{souls:{total:2,healthy:2,degraded:0}},'100%']])('observed ratio %j is %s',(stats,value)=>{
 mocks.stats=stats;mount();expect(balance()).toHaveTextContent(value);expect(balance()).toHaveTextContent('Current soul health');expect(balance()).not.toHaveTextContent('Last 30 days')
})
