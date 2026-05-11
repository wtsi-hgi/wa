const globalWithReactActEnvironment = globalThis as typeof globalThis & {
    IS_REACT_ACT_ENVIRONMENT?: boolean;
};

globalWithReactActEnvironment.IS_REACT_ACT_ENVIRONMENT = true;
