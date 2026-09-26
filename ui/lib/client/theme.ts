/** Shared by the root layout (server) and the theme switch (client); keep free of 'use client'. */
export const THEME_STORAGE_KEY = 'heliostat-theme';

/** Runs before first paint (inlined in the root layout) so a saved theme never flashes. */
export const THEME_BOOTSTRAP = `try{var t=localStorage.getItem('${THEME_STORAGE_KEY}');if(t==='light'||t==='dark')document.documentElement.dataset.theme=t}catch(e){}`;
