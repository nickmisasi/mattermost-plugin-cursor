// Mock for Jest tests - react-router-dom is provided by Mattermost host at runtime.
export const useHistory = () => ({push: jest.fn(), replace: jest.fn()});
export const MemoryRouter = (props: {children: unknown}) => props.children;
export const useLocation = () => ({pathname: '/', search: ''});
