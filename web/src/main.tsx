// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { createBrowserRouter, RouterProvider } from 'react-router-dom'
import { ApiError } from './api/client'
import { App } from './app/App'
import './theme/base.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // A 4xx will not get better by asking again; a network hiccup might.
      retry: (count, err) => !(err instanceof ApiError && err.status >= 400 && err.status < 500) && count < 2,
      staleTime: 60_000,
    },
  },
})

// A data router around the app's own routes, so a page can ask before it is left with unsaved
// changes (useBlocker: the workout editor).
const router = createBrowserRouter([{ path: '*', element: <App /> }])

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
)
