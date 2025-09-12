export default class Router {
    constructor() {
        this.routes = [];
    }

    add(path, handler) {
        this.routes.push({ path, handler });
        return this; // for chaining
    }

    match(path) {
        for (const route of this.routes) {
            if (typeof route.path === 'string' && route.path === path) {
                return { handler: route.handler, params: [] };
            } else if (route.path instanceof RegExp) {
                const match = path.match(route.path);
                if (match) {
                    return { handler: route.handler, params: match.slice(1) };
                }
            }
        }
        return null;
    }

    resolve() {
        const path = window.location.pathname;
        const matched = this.match(path);
        if (matched) {
            // Pass the app instance as the first argument to the handler
            matched.handler.apply(null, matched.params);
        }
    }

    navigateTo(path) {
        history.pushState({}, '', path);
        this.resolve();
    }
}
