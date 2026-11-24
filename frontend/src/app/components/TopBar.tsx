'use client'

import GlobalMenu from './GlobalMenu'
import TeamScopeSelector from './TeamScopeSelector'

interface TopBarProps { title?: string; subtitle?: string; showFilterButton?: boolean; filterButtonText?: string; onFilterToggle?: () => void; showSettingsButton?: boolean; showNewSessionButton?: boolean; onNewSession?: () => void; children?: React.ReactNode }

function HeaderActions({ props }: { props: TopBarProps }) {
  return <div className="flex items-center gap-2 flex-shrink-0">
    {props.showFilterButton && props.onFilterToggle && <button onClick={props.onFilterToggle} className="inline-flex items-center px-3 py-2 text-sm bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors"><svg className="w-4 h-4 sm:mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 4a1 1 0 011-1h16a1 1 0 011 1v2.586a1 1 0 01-.293.707l-6.414 6.414a1 1 0 00-.293.707V17l-4 4v-6.586a1 1 0 00-.293-.707L3.293 7.414A1 1 0 013 6.707V4z" /></svg><span className="hidden sm:inline">{props.filterButtonText}</span></button>}
    {props.showNewSessionButton && props.onNewSession && <button onClick={props.onNewSession} className="inline-flex items-center px-4 py-2 bg-main-color-dark hover:bg-main-color text-white font-medium rounded-md transition-colors focus:outline-none focus:ring-2 focus:ring-main-color focus:ring-offset-2 shadow-sm"><svg className="w-4 h-4 sm:mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" /></svg><span className="hidden sm:inline">新しいセッション</span><span className="sm:hidden">新規</span></button>}
    <TeamScopeSelector />
    {props.showSettingsButton && <GlobalMenu />}
  </div>
}

export default function TopBar({ title, subtitle, showFilterButton = false, filterButtonText = 'フィルタを表示', onFilterToggle, showSettingsButton = true, showNewSessionButton = false, onNewSession, children }: TopBarProps) {
  const props = { showFilterButton, filterButtonText, onFilterToggle, showSettingsButton, showNewSessionButton, onNewSession }
  return <div className="sticky top-0 z-40 bg-white dark:bg-gray-800 shadow-sm border-b border-gray-200 dark:border-gray-700 relative"><div className="relative px-4 md:px-6 lg:px-8 py-3 md:py-4"><div className="flex items-center justify-between gap-4"><div className="flex-1">{title && <h1 className="text-xl sm:text-2xl font-bold text-gray-900 dark:text-white">{title}</h1>}{subtitle && <p className="text-gray-600 dark:text-gray-400 mt-1">{subtitle}</p>}</div><HeaderActions props={props} /></div>{children && <div className="mt-3">{children}</div>}</div></div>
}
