import {render,screen,cleanup} from '@testing-library/react'
import {MemoryRouter} from 'react-router-dom'
import {afterEach,expect,it,vi} from 'vitest'
import {Sidebar} from './Sidebar'
vi.mock('../api/hooks',()=>({useAuth:()=>({logout:vi.fn()})}))
afterEach(cleanup)
it.each([['/souls',true],['/souls/s1',true],['/souls/s1/edit',true],['/soulship',false],['/',false]])('souls active at %s is %s',(path,active)=>{
 render(<MemoryRouter initialEntries={[path as string]}><Sidebar/></MemoryRouter>)
 expect(screen.getByRole('link',{name:/Essence/}).classList.contains('text-[#F4D03F]')).toBe(active)
})
it('root is inactive on a child route',()=>{
 render(<MemoryRouter initialEntries={['/souls/s1']}><Sidebar/></MemoryRouter>)
 expect(screen.getByRole('link',{name:/Hall of Judgment/})).not.toHaveClass('text-[#F4D03F]')
})
