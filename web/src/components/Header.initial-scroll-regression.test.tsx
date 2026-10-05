import {render, cleanup} from '@testing-library/react'
import {afterEach, expect, it, vi} from 'vitest'
import {Header} from './Header'
vi.mock('../api/hooks',()=>({useAuth:()=>({user:null,logout:vi.fn()})}))
vi.mock('../stores/themeStore',()=>({useThemeStore:()=>({theme:'dark',setTheme:vi.fn()}),getEffectiveTheme:()=> 'dark',applyTheme:vi.fn()}))
vi.mock('./WorkspaceSwitcher',()=>({WorkspaceSwitcher:()=>null}))
vi.mock('react-router-dom',()=>({useNavigate:()=>vi.fn()}))
afterEach(cleanup)
it.each([0,10,11,20])('initial scroll %s respects threshold',(y)=>{
 Object.defineProperty(window,'scrollY',{configurable:true,value:y})
 const v=render(<Header/>);expect(v.container.querySelector('header')!.classList.contains('bg-gray-950/90')).toBe(y>10)
})
