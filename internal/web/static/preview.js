var Qe=(function(){"use strict";let o=()=>{},e={morphStyle:"outerHTML",callbacks:{beforeNodeAdded:o,afterNodeAdded:o,beforeNodeMorphed:o,afterNodeMorphed:o,beforeNodeRemoved:o,afterNodeRemoved:o,beforeAttributeUpdated:o},head:{style:"merge",shouldPreserve:b=>b.getAttribute("im-preserve")==="true",shouldReAppend:b=>b.getAttribute("im-re-append")==="true",shouldRemove:o,afterHeadMorphed:o},restoreFocus:!0};function t(b,w,$={}){b=f(b);let m=v(w),S=p(b,m,$),x=r(S,()=>u(S,b,m,l=>l.morphStyle==="innerHTML"?(i(l,b,m),Array.from(b.childNodes)):n(l,b,m)));return S.pantry.remove(),x}function n(b,w,$){let m=v(w);return i(b,m,$,w,w.nextSibling),Array.from(m.childNodes)}function r(b,w){if(!b.config.restoreFocus)return w();let $=document.activeElement;if(!($ instanceof HTMLInputElement||$ instanceof HTMLTextAreaElement))return w();let{id:m,selectionStart:S,selectionEnd:x}=$,l=w();return m&&m!==document.activeElement?.getAttribute("id")&&($=b.target.querySelector(`[id="${m}"]`),$?.focus()),$&&!$.selectionEnd&&x&&$.setSelectionRange(S,x),l}let i=(function(){function b(d,h,_,g=null,E=null){h instanceof HTMLTemplateElement&&_ instanceof HTMLTemplateElement&&(h=h.content,_=_.content),g||=h.firstChild;for(let A of _.childNodes){if(g&&g!=E){let O=$(d,A,g,E);if(O){O!==g&&S(d,g,O),s(O,A,d),g=O.nextSibling;continue}}if(A instanceof Element){let O=A.getAttribute("id");if(d.persistentIds.has(O)){let R=x(h,O,g,d);s(R,A,d),g=R.nextSibling;continue}}let C=w(h,A,g,d);C&&(g=C.nextSibling)}for(;g&&g!=E;){let A=g;g=g.nextSibling,m(d,A)}}function w(d,h,_,g){if(g.callbacks.beforeNodeAdded(h)===!1)return null;if(g.idMap.has(h)){let E=document.createElement(h.tagName);return d.insertBefore(E,_),s(E,h,g),g.callbacks.afterNodeAdded(E),E}else{let E=document.importNode(h,!0);return d.insertBefore(E,_),g.callbacks.afterNodeAdded(E),E}}let $=(function(){function d(g,E,A,C){let O=null,R=E.nextSibling,We=0,M=A;for(;M&&M!=C;){if(_(M,E)){if(h(g,M,E))return M;O===null&&(g.idMap.has(M)||(O=M))}if(O===null&&R&&_(M,R)&&(We++,R=R.nextSibling,We>=2&&(O=void 0)),g.activeElementAndParents.includes(M))break;M=M.nextSibling}return O||null}function h(g,E,A){let C=g.idMap.get(E),O=g.idMap.get(A);if(!O||!C)return!1;for(let R of C)if(O.has(R))return!0;return!1}function _(g,E){let A=g,C=E;return A.nodeType===C.nodeType&&A.tagName===C.tagName&&(!A.getAttribute?.("id")||A.getAttribute?.("id")===C.getAttribute?.("id"))}return d})();function m(d,h){if(d.idMap.has(h))y(d.pantry,h,null);else{if(d.callbacks.beforeNodeRemoved(h)===!1)return;h.parentNode?.removeChild(h),d.callbacks.afterNodeRemoved(h)}}function S(d,h,_){let g=h;for(;g&&g!==_;){let E=g;g=g.nextSibling,m(d,E)}return g}function x(d,h,_,g){let E=g.target.getAttribute?.("id")===h&&g.target||g.target.querySelector(`[id="${h}"]`)||g.pantry.querySelector(`[id="${h}"]`);return l(E,g),y(d,E,_),E}function l(d,h){let _=d.getAttribute("id");for(;d=d.parentNode;){let g=h.idMap.get(d);g&&(g.delete(_),g.size||h.idMap.delete(d))}}function y(d,h,_){if(d.moveBefore)try{d.moveBefore(h,_)}catch{d.insertBefore(h,_)}else d.insertBefore(h,_)}return b})(),s=(function(){function b(l,y,d){return d.ignoreActive&&l===document.activeElement?null:(d.callbacks.beforeNodeMorphed(l,y)===!1||(l instanceof HTMLHeadElement&&d.head.ignore||(l instanceof HTMLHeadElement&&d.head.style!=="morph"?a(l,y,d):(w(l,y,d),x(l,d)||i(d,l,y))),d.callbacks.afterNodeMorphed(l,y)),l)}function w(l,y,d){let h=y.nodeType;if(h===1){let _=l,g=y,E=_.attributes,A=g.attributes;for(let C of A)S(C.name,_,"update",d)||_.getAttribute(C.name)!==C.value&&_.setAttribute(C.name,C.value);for(let C=E.length-1;0<=C;C--){let O=E[C];if(O&&!g.hasAttribute(O.name)){if(S(O.name,_,"remove",d))continue;_.removeAttribute(O.name)}}x(_,d)||$(_,g,d)}(h===8||h===3)&&l.nodeValue!==y.nodeValue&&(l.nodeValue=y.nodeValue)}function $(l,y,d){if(l instanceof HTMLInputElement&&y instanceof HTMLInputElement&&y.type!=="file"){let h=y.value,_=l.value;m(l,y,"checked",d),m(l,y,"disabled",d),y.hasAttribute("value")?_!==h&&(S("value",l,"update",d)||(l.setAttribute("value",h),l.value=h)):S("value",l,"remove",d)||(l.value="",l.removeAttribute("value"))}else if(l instanceof HTMLOptionElement&&y instanceof HTMLOptionElement)m(l,y,"selected",d);else if(l instanceof HTMLTextAreaElement&&y instanceof HTMLTextAreaElement){let h=y.value,_=l.value;if(S("value",l,"update",d))return;h!==_&&(l.value=h),l.firstChild&&l.firstChild.nodeValue!==h&&(l.firstChild.nodeValue=h)}}function m(l,y,d,h){let _=y[d],g=l[d];if(_!==g){let E=S(d,l,"update",h);E||(l[d]=y[d]),_?E||l.setAttribute(d,""):S(d,l,"remove",h)||l.removeAttribute(d)}}function S(l,y,d,h){return l==="value"&&h.ignoreActiveValue&&y===document.activeElement?!0:h.callbacks.beforeAttributeUpdated(l,y,d)===!1}function x(l,y){return!!y.ignoreActiveValue&&l===document.activeElement&&l!==document.body}return b})();function u(b,w,$,m){if(b.head.block){let S=w.querySelector("head"),x=$.querySelector("head");if(S&&x){let l=a(S,x,b);return Promise.all(l).then(()=>{let y=Object.assign(b,{head:{block:!1,ignore:!0}});return m(y)})}}return m(b)}function a(b,w,$){let m=[],S=[],x=[],l=[],y=new Map;for(let h of w.children)y.set(h.outerHTML,h);for(let h of b.children){let _=y.has(h.outerHTML),g=$.head.shouldReAppend(h),E=$.head.shouldPreserve(h);_||E?g?S.push(h):(y.delete(h.outerHTML),x.push(h)):$.head.style==="append"?g&&(S.push(h),l.push(h)):$.head.shouldRemove(h)!==!1&&S.push(h)}l.push(...y.values());let d=[];for(let h of l){let _=document.createRange().createContextualFragment(h.outerHTML).firstChild;if($.callbacks.beforeNodeAdded(_)!==!1){if("href"in _&&_.href||"src"in _&&_.src){let g,E=new Promise(function(A){g=A});_.addEventListener("load",function(){g()}),d.push(E)}b.appendChild(_),$.callbacks.afterNodeAdded(_),m.push(_)}}for(let h of S)$.callbacks.beforeNodeRemoved(h)!==!1&&(b.removeChild(h),$.callbacks.afterNodeRemoved(h));return $.head.afterHeadMorphed(b,{added:m,kept:x,removed:S}),d}let p=(function(){function b(d,h,_){let{persistentIds:g,idMap:E}=l(d,h),A=w(_),C=A.morphStyle||"outerHTML";if(!["innerHTML","outerHTML"].includes(C))throw`Do not understand how to morph style ${C}`;return{target:d,newContent:h,config:A,morphStyle:C,ignoreActive:A.ignoreActive,ignoreActiveValue:A.ignoreActiveValue,restoreFocus:A.restoreFocus,idMap:E,persistentIds:g,pantry:$(),activeElementAndParents:m(d),callbacks:A.callbacks,head:A.head}}function w(d){let h=Object.assign({},e);return Object.assign(h,d),h.callbacks=Object.assign({},e.callbacks,d.callbacks),h.head=Object.assign({},e.head,d.head),h}function $(){let d=document.createElement("div");return d.hidden=!0,document.body.insertAdjacentElement("afterend",d),d}function m(d){let h=[],_=document.activeElement;if(_?.tagName!=="BODY"&&d.contains(_))for(;_&&(h.push(_),_!==d);)_=_.parentElement;return h}function S(d){let h=Array.from(d.querySelectorAll("[id]"));return d.getAttribute?.("id")&&h.push(d),h}function x(d,h,_,g){for(let E of g){let A=E.getAttribute("id");if(h.has(A)){let C=E;for(;C;){let O=d.get(C);if(O==null&&(O=new Set,d.set(C,O)),O.add(A),C===_)break;C=C.parentElement}}}}function l(d,h){let _=S(d),g=S(h),E=y(_,g),A=new Map;x(A,E,d,_);let C=h.__idiomorphRoot||h;return x(A,E,C,g),{persistentIds:E,idMap:A}}function y(d,h){let _=new Set,g=new Map;for(let{id:A,tagName:C}of d)g.has(A)?_.add(A):g.set(A,C);let E=new Set;for(let{id:A,tagName:C}of h)E.has(A)?_.add(A):g.get(A)===C&&E.add(A);for(let A of _)E.delete(A);return E}return b})(),{normalizeElement:f,normalizeParent:v}=(function(){let b=new WeakSet;function w(x){return x instanceof Document?x.documentElement:x}function $(x){if(x==null)return document.createElement("div");if(typeof x=="string")return $(S(x));if(b.has(x))return x;if(x instanceof Node){if(x.parentNode)return new m(x);{let l=document.createElement("div");return l.append(x),l}}else{let l=document.createElement("div");for(let y of[...x])l.append(y);return l}}class m{constructor(l){this.originalNode=l,this.realParentNode=l.parentNode,this.previousSibling=l.previousSibling,this.nextSibling=l.nextSibling}get childNodes(){let l=[],y=this.previousSibling?this.previousSibling.nextSibling:this.realParentNode.firstChild;for(;y&&y!=this.nextSibling;)l.push(y),y=y.nextSibling;return l}querySelectorAll(l){return this.childNodes.reduce((y,d)=>{if(d instanceof Element){d.matches(l)&&y.push(d);let h=d.querySelectorAll(l);for(let _=0;_<h.length;_++)y.push(h[_])}return y},[])}insertBefore(l,y){return this.realParentNode.insertBefore(l,y)}moveBefore(l,y){return this.realParentNode.moveBefore(l,y)}get __idiomorphRoot(){return this.originalNode}}function S(x){let l=new DOMParser,y=x.replace(/<svg(\s[^>]*>|>)([\s\S]*?)<\/svg>/gim,"");if(y.match(/<\/html>/)||y.match(/<\/head>/)||y.match(/<\/body>/)){let d=l.parseFromString(x,"text/html");if(y.match(/<\/html>/))return b.add(d),d;{let h=d.firstChild;return h&&b.add(h),h}}else{let h=l.parseFromString("<body><template>"+x+"</template></body>","text/html").body.querySelector("template").content;return b.add(h),h}}return{normalizeElement:w,normalizeParent:$}})();return{morph:t,defaults:e}})();function W(o){return o?o.charAt(0)==="/"?o:"/"+o:"/"}function T(){var o=document.querySelector("base");if(!o||!o.href)return"";var e=new URL(o.href,window.location.href).pathname;return e==="/"?"":e.replace(/\/+$/,"")}function ie(){var o=T(),e=window.location.pathname||"/";return o&&(e===o||e.indexOf(o+"/")===0)&&(e=e.slice(o.length)),W(e)}function Je(){return customElements.get("bn-context")?Promise.resolve():customElements.whenDefined("bn-context")}function Ye(o,e){if(!o)return console.debug("bino: swapContext skipped - empty html"),!1;e||(e=new DOMParser);var t=e.parseFromString(o,"text/html"),n=t.querySelector("bn-context"),r=document.querySelector("bn-context");return n?r?(Ct(r,n),Qe.morph(r,n.innerHTML,{morphStyle:"innerHTML",callbacks:{beforeAttributeUpdated:function(i,s,u){if(i==="class"&&s.tagName&&s.tagName.includes("-"))return!1}}}),!0):(console.debug("bino: swapContext skipped - live DOM has no <bn-context>"),!1):(console.debug("bino: swapContext skipped - incoming HTML has no <bn-context>"),!1)}function Ct(o,e){for(var t=0;t<e.attributes.length;t++){var n=e.attributes[t];n.name!=="class"&&o.getAttribute(n.name)!==n.value&&o.setAttribute(n.name,n.value)}for(var r=o.attributes.length-1;r>=0;r--){var i=o.attributes[r].name;i!=="class"&&(e.hasAttribute(i)||o.removeAttribute(i))}}var Xe=[["rowGroup","data-bn-row-group"],["category","data-bn-category"],["subCategory","data-bn-sub-category"],["date","data-bn-date"],["scenario","data-bn-scenario"],["row","data-bn-row"]],Ot=["Text","Tree","Grid","SmallMultiple"],zt=["bn-chart-time","bn-chart-structure","bn-chart-scatter","bn-chart-bubble","bn-chart-bullet","bn-table"],Ze=o=>!!o&&o.nodeType===1;function Lt(o){let e=o.getAttribute("data-bn-part");if(!e)return null;let t={};for(let[n,r]of Xe){let i=o.getAttribute(r);i!==null&&i!==""&&(t[n]=i)}return e!=="element"&&(t.part=e),t}function Tt(o){for(let e of o){let t=Lt(e);if(t)return{point:t,stopped:!1};if(e.localName==="td"||e.localName==="th")return{point:null,stopped:!0}}return{point:null,stopped:!1}}var Ge=o=>o.hasAttribute("data-bino-kind")&&!!(o.getAttribute("data-bino-ref")||o.getAttribute("data-bino-name")),Mt=o=>o.localName==="bn-grid"||o.localName==="bn-tree";function ve(o){let e=o.filter(Ze),t=e.findIndex(v=>v.hasAttribute("data-bino-target-kind"));if(t<0){if(t=e.findIndex(Ge),t<0)return null;let v=e.findIndex((b,w)=>w>t&&Mt(b));if(v>=0){let b=e.findIndex((w,$)=>$>v&&Ge(w));b>=0&&(t=b)}}let n=e[t],r=n.hasAttribute("data-bino-target-kind"),i=r?n.getAttribute("data-bino-target-kind"):n.getAttribute("data-bino-kind"),s=r?n.getAttribute("data-bino-target-name"):n.getAttribute("data-bino-ref")||n.getAttribute("data-bino-name"),u=e.slice(0,t),a={kind:i,name:s,slot:n,inner:u,host:[...u].reverse().find(v=>zt.includes(v.localName))};if(i==="SmallMultiple"){let v=u.find(b=>b.hasAttribute("data-bino-panel"));v&&(a.panel=v.getAttribute("data-bino-panel"))}let p=e.slice(t).find(v=>v.hasAttribute("data-bino-source-line")),f=p?parseInt(p.getAttribute("data-bino-source-line")||"",10):0;return f>0&&(a.sourceFile=p.getAttribute("data-bino-source-file")||"",a.line=f),a}function Rt(o,e,t,n){let r=e.getBoundingClientRect(),i=t-r.left,s=n-r.top,u=null,a=1/0;for(let f of o&&o.elements||[]){let v=f.rect&&f.rect.component;if(!f.point||!v||i<v.x||i>v.x+v.width||s<v.y||s>v.y+v.height)continue;let b=v.width*v.height;b<a&&(a=b,u=f.point)}if(!u)return null;let p={};for(let[f]of Xe)u[f]!==void 0&&u[f]!==null&&u[f]!==""&&(p[f]=String(u[f]));return u.part&&u.part!=="element"&&(p.part=u.part),p}function Dt(o,e){if(!o||typeof o.getLayoutState!="function")return Promise.resolve(null);let t;return Promise.race([o.getLayoutState({detail:"full"}),new Promise(n=>{t=setTimeout(()=>n(null),e)})]).catch(()=>null).finally(()=>clearTimeout(t))}async function Nt(o,e,t){let n=ve(o);if(!n)return null;let r={kind:n.kind,name:n.name};n.panel&&(r.panel=n.panel),n.sourceFile!==void 0&&(r.sourceFile=n.sourceFile,r.line=n.line);let i=n.kind==="SmallMultiple"&&n.panel?"panel":n.kind;if(Ot.includes(i))return r;let s=Tt(n.inner);if(s.stopped)return r;let u=s.point;if(!u&&n.host){let a=await Dt(n.host,1e3);u=Rt(a,n.host,e,t)}return u&&Object.keys(u).length&&(r.point=u),r}function Pt(o,e,t){let n=o.filter(Ze),r=n.find(p=>p.hasAttribute("data-bino-annotation"));if(!r)return null;if(n[0]&&n[0].classList&&n[0].classList.contains("hit")&&e!==void 0){let p=r.getRootNode(),v=(p&&p.elementsFromPoint?p.elementsFromPoint(e,t):[]).find(b=>!b.classList.contains("hit")&&b.closest&&b.closest("[data-bino-annotation]"));v&&(r=v.closest("[data-bino-annotation]"))}let i=r.getAttribute("data-bino-annotation"),s={name:i},u=r.getRootNode&&r.getRootNode(),a=u&&u.host;try{let f=JSON.parse(a&&a.getAttribute("items")||"[]").find(v=>v&&v.name===i);f&&f.line>0&&(s.sourceFile=f.sourceFile||"",s.line=f.line)}catch{}return s}function It(o){let e=[];return o.querySelectorAll("bn-layout-page").forEach(t=>{let n=t.querySelector("bn-annotation-layer");if(!n)return;let r=[];try{r=JSON.parse(n.getAttribute("items")||"[]")}catch{return}let i=n.shadowRoot,s=new Set,u=[];for(let a of r){if(!a||!a.name||s.has(a.name))continue;s.add(a.name);let p=!!(i&&i.querySelector('[data-bino-annotation="'+CSS.escape(a.name)+'"]'));u.push({name:a.name,label:a.label||"",placed:p})}e.push({page:t.getAttribute("data-bino-page")||"",items:u})}),e}function oe(o){let e=o.customElements&&o.customElements.get("bn-annotation-layer");return!!e&&"interactive"in e.prototype}function et(o){let e=o||window;if(e.__binoAnnotate||e.parent===e)return;e.__binoAnnotate=!0;let t=e.document,n=!1,r=0,i=null,s=()=>(i||(i=t.createElement("div"),i.setAttribute("data-bino-annotate-outline",""),i.style.cssText="position:fixed;pointer-events:none;z-index:2147483647;outline:2px dashed #0070c0;outline-offset:1px;display:none",t.body.appendChild(i)),i),u=()=>{i&&(i.style.display="none")},a=m=>{let S=m.getBoundingClientRect(),x=s();x.style.left=S.left+"px",x.style.top=S.top+"px",x.style.width=S.width+"px",x.style.height=S.height+"px",x.style.display="block"},p=()=>{t.querySelectorAll("bn-annotation-layer").forEach(m=>{n?m.setAttribute("interactive",""):m.removeAttribute("interactive")})},f="",v=new Set,b=()=>{let m=It(t),S=JSON.stringify(m);S!==f&&(f=S,e.parent.postMessage({type:"bino:annotations",pages:m,marks:oe(e)?1:0},"*"))},w=()=>{for(let m of[800,3e3])v.has(m)||(v.add(m),setTimeout(()=>{v.delete(m),b()},m))};new e.MutationObserver(()=>{n&&p(),w()}).observe(t.documentElement,{childList:!0,subtree:!0}),e.addEventListener("message",m=>{!m.data||m.data.type!=="bino:annotateMode"||(n=!!m.data.on,t.documentElement.style.cursor=n?"crosshair":"",p(),n||u())}),t.addEventListener("mousemove",m=>{if(!n)return;let S=m.composedPath(),x=ve(S);if(!x){u();return}let l=null;for(let y of x.inner){if(y.hasAttribute("data-bn-part")){l=y;break}if(y.localName==="td"||y.localName==="th")break}a(l||x.slot)},!0),t.addEventListener("click",m=>{if(!n||m.button!==0)return;let S=Pt(m.composedPath(),m.clientX,m.clientY);if(S){m.preventDefault(),m.stopImmediatePropagation(),e.parent.postMessage(Object.assign({type:"bino:revealAnnotation",source:m.metaKey||m.ctrlKey},S),"*");return}if(m.metaKey||m.ctrlKey||!ve(m.composedPath()))return;m.preventDefault(),m.stopPropagation();let x=++r;Nt(m.composedPath(),m.clientX,m.clientY).then(l=>{x!==r||!l||e.parent.postMessage(Object.assign({type:"bino:pickPoint"},l),"*")})},!0);let $=()=>e.parent.postMessage({type:"bino:capabilities",annotate:1,marks:oe(e)?1:0},"*");$(),e.customElements&&!oe(e)&&e.customElements.whenDefined("bn-annotation-layer").then(()=>{oe(e)&&$()}),w()}var se=globalThis,ae=se.ShadowRoot&&(se.ShadyCSS===void 0||se.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,me=Symbol(),tt=new WeakMap,Q=class{constructor(e,t,n){if(this._$cssResult$=!0,n!==me)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=e,this.t=t}get styleSheet(){let e=this.o,t=this.t;if(ae&&e===void 0){let n=t!==void 0&&t.length===1;n&&(e=tt.get(t)),e===void 0&&((this.o=e=new CSSStyleSheet).replaceSync(this.cssText),n&&tt.set(t,e))}return e}toString(){return this.cssText}},nt=o=>new Q(typeof o=="string"?o:o+"",void 0,me),L=(o,...e)=>{let t=o.length===1?o[0]:e.reduce((n,r,i)=>n+(s=>{if(s._$cssResult$===!0)return s.cssText;if(typeof s=="number")return s;throw Error("Value passed to 'css' function must be a 'css' function result: "+s+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(r)+o[i+1],o[0]);return new Q(t,o,me)},rt=(o,e)=>{if(ae)o.adoptedStyleSheets=e.map(t=>t instanceof CSSStyleSheet?t:t.styleSheet);else for(let t of e){let n=document.createElement("style"),r=se.litNonce;r!==void 0&&n.setAttribute("nonce",r),n.textContent=t.cssText,o.appendChild(n)}},ge=ae?o=>o:o=>o instanceof CSSStyleSheet?(e=>{let t="";for(let n of e.cssRules)t+=n.cssText;return nt(t)})(o):o;var{is:Ht,defineProperty:qt,getOwnPropertyDescriptor:Ut,getOwnPropertyNames:Bt,getOwnPropertySymbols:Kt,getPrototypeOf:jt}=Object,le=globalThis,it=le.trustedTypes,Ft=it?it.emptyScript:"",Vt=le.reactiveElementPolyfillSupport,J=(o,e)=>o,_e={toAttribute(o,e){switch(e){case Boolean:o=o?Ft:null;break;case Object:case Array:o=o==null?o:JSON.stringify(o)}return o},fromAttribute(o,e){let t=o;switch(e){case Boolean:t=o!==null;break;case Number:t=o===null?null:Number(o);break;case Object:case Array:try{t=JSON.parse(o)}catch{t=null}}return t}},st=(o,e)=>!Ht(o,e),ot={attribute:!0,type:String,converter:_e,reflect:!1,useDefault:!1,hasChanged:st};Symbol.metadata??=Symbol("metadata"),le.litPropertyMetadata??=new WeakMap;var D=class extends HTMLElement{static addInitializer(e){this._$Ei(),(this.l??=[]).push(e)}static get observedAttributes(){return this.finalize(),this._$Eh&&[...this._$Eh.keys()]}static createProperty(e,t=ot){if(t.state&&(t.attribute=!1),this._$Ei(),this.prototype.hasOwnProperty(e)&&((t=Object.create(t)).wrapped=!0),this.elementProperties.set(e,t),!t.noAccessor){let n=Symbol(),r=this.getPropertyDescriptor(e,n,t);r!==void 0&&qt(this.prototype,e,r)}}static getPropertyDescriptor(e,t,n){let{get:r,set:i}=Ut(this.prototype,e)??{get(){return this[t]},set(s){this[t]=s}};return{get:r,set(s){let u=r?.call(this);i?.call(this,s),this.requestUpdate(e,u,n)},configurable:!0,enumerable:!0}}static getPropertyOptions(e){return this.elementProperties.get(e)??ot}static _$Ei(){if(this.hasOwnProperty(J("elementProperties")))return;let e=jt(this);e.finalize(),e.l!==void 0&&(this.l=[...e.l]),this.elementProperties=new Map(e.elementProperties)}static finalize(){if(this.hasOwnProperty(J("finalized")))return;if(this.finalized=!0,this._$Ei(),this.hasOwnProperty(J("properties"))){let t=this.properties,n=[...Bt(t),...Kt(t)];for(let r of n)this.createProperty(r,t[r])}let e=this[Symbol.metadata];if(e!==null){let t=litPropertyMetadata.get(e);if(t!==void 0)for(let[n,r]of t)this.elementProperties.set(n,r)}this._$Eh=new Map;for(let[t,n]of this.elementProperties){let r=this._$Eu(t,n);r!==void 0&&this._$Eh.set(r,t)}this.elementStyles=this.finalizeStyles(this.styles)}static finalizeStyles(e){let t=[];if(Array.isArray(e)){let n=new Set(e.flat(1/0).reverse());for(let r of n)t.unshift(ge(r))}else e!==void 0&&t.push(ge(e));return t}static _$Eu(e,t){let n=t.attribute;return n===!1?void 0:typeof n=="string"?n:typeof e=="string"?e.toLowerCase():void 0}constructor(){super(),this._$Ep=void 0,this.isUpdatePending=!1,this.hasUpdated=!1,this._$Em=null,this._$Ev()}_$Ev(){this._$ES=new Promise(e=>this.enableUpdating=e),this._$AL=new Map,this._$E_(),this.requestUpdate(),this.constructor.l?.forEach(e=>e(this))}addController(e){(this._$EO??=new Set).add(e),this.renderRoot!==void 0&&this.isConnected&&e.hostConnected?.()}removeController(e){this._$EO?.delete(e)}_$E_(){let e=new Map,t=this.constructor.elementProperties;for(let n of t.keys())this.hasOwnProperty(n)&&(e.set(n,this[n]),delete this[n]);e.size>0&&(this._$Ep=e)}createRenderRoot(){let e=this.shadowRoot??this.attachShadow(this.constructor.shadowRootOptions);return rt(e,this.constructor.elementStyles),e}connectedCallback(){this.renderRoot??=this.createRenderRoot(),this.enableUpdating(!0),this._$EO?.forEach(e=>e.hostConnected?.())}enableUpdating(e){}disconnectedCallback(){this._$EO?.forEach(e=>e.hostDisconnected?.())}attributeChangedCallback(e,t,n){this._$AK(e,n)}_$ET(e,t){let n=this.constructor.elementProperties.get(e),r=this.constructor._$Eu(e,n);if(r!==void 0&&n.reflect===!0){let i=(n.converter?.toAttribute!==void 0?n.converter:_e).toAttribute(t,n.type);this._$Em=e,i==null?this.removeAttribute(r):this.setAttribute(r,i),this._$Em=null}}_$AK(e,t){let n=this.constructor,r=n._$Eh.get(e);if(r!==void 0&&this._$Em!==r){let i=n.getPropertyOptions(r),s=typeof i.converter=="function"?{fromAttribute:i.converter}:i.converter?.fromAttribute!==void 0?i.converter:_e;this._$Em=r;let u=s.fromAttribute(t,i.type);this[r]=u??this._$Ej?.get(r)??u,this._$Em=null}}requestUpdate(e,t,n,r=!1,i){if(e!==void 0){let s=this.constructor;if(r===!1&&(i=this[e]),n??=s.getPropertyOptions(e),!((n.hasChanged??st)(i,t)||n.useDefault&&n.reflect&&i===this._$Ej?.get(e)&&!this.hasAttribute(s._$Eu(e,n))))return;this.C(e,t,n)}this.isUpdatePending===!1&&(this._$ES=this._$EP())}C(e,t,{useDefault:n,reflect:r,wrapped:i},s){n&&!(this._$Ej??=new Map).has(e)&&(this._$Ej.set(e,s??t??this[e]),i!==!0||s!==void 0)||(this._$AL.has(e)||(this.hasUpdated||n||(t=void 0),this._$AL.set(e,t)),r===!0&&this._$Em!==e&&(this._$Eq??=new Set).add(e))}async _$EP(){this.isUpdatePending=!0;try{await this._$ES}catch(t){Promise.reject(t)}let e=this.scheduleUpdate();return e!=null&&await e,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){if(!this.isUpdatePending)return;if(!this.hasUpdated){if(this.renderRoot??=this.createRenderRoot(),this._$Ep){for(let[r,i]of this._$Ep)this[r]=i;this._$Ep=void 0}let n=this.constructor.elementProperties;if(n.size>0)for(let[r,i]of n){let{wrapped:s}=i,u=this[r];s!==!0||this._$AL.has(r)||u===void 0||this.C(r,void 0,i,u)}}let e=!1,t=this._$AL;try{e=this.shouldUpdate(t),e?(this.willUpdate(t),this._$EO?.forEach(n=>n.hostUpdate?.()),this.update(t)):this._$EM()}catch(n){throw e=!1,this._$EM(),n}e&&this._$AE(t)}willUpdate(e){}_$AE(e){this._$EO?.forEach(t=>t.hostUpdated?.()),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(e)),this.updated(e)}_$EM(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$ES}shouldUpdate(e){return!0}update(e){this._$Eq&&=this._$Eq.forEach(t=>this._$ET(t,this[t])),this._$EM()}updated(e){}firstUpdated(e){}};D.elementStyles=[],D.shadowRootOptions={mode:"open"},D[J("elementProperties")]=new Map,D[J("finalized")]=new Map,Vt?.({ReactiveElement:D}),(le.reactiveElementVersions??=[]).push("2.1.2");var Se=globalThis,at=o=>o,de=Se.trustedTypes,lt=de?de.createPolicy("lit-html",{createHTML:o=>o}):void 0,bt="$lit$",P=`lit$${Math.random().toFixed(9).slice(2)}$`,ft="?"+P,Wt=`<${ft}>`,U=document,G=()=>U.createComment(""),X=o=>o===null||typeof o!="object"&&typeof o!="function",Ae=Array.isArray,Qt=o=>Ae(o)||typeof o?.[Symbol.iterator]=="function",ye=`[ 	
\f\r]`,Y=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,dt=/-->/g,ct=/>/g,H=RegExp(`>|${ye}(?:([^\\s"'>=/]+)(${ye}*=${ye}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),ut=/'/g,ht=/"/g,vt=/^(?:script|style|textarea|title)$/i,Ce=o=>(e,...t)=>({_$litType$:o,strings:e,values:t}),c=Ce(1),Oe=Ce(2),Cn=Ce(3),B=Symbol.for("lit-noChange"),k=Symbol.for("lit-nothing"),pt=new WeakMap,q=U.createTreeWalker(U,129);function mt(o,e){if(!Ae(o)||!o.hasOwnProperty("raw"))throw Error("invalid template strings array");return lt!==void 0?lt.createHTML(e):e}var Jt=(o,e)=>{let t=o.length-1,n=[],r,i=e===2?"<svg>":e===3?"<math>":"",s=Y;for(let u=0;u<t;u++){let a=o[u],p,f,v=-1,b=0;for(;b<a.length&&(s.lastIndex=b,f=s.exec(a),f!==null);)b=s.lastIndex,s===Y?f[1]==="!--"?s=dt:f[1]!==void 0?s=ct:f[2]!==void 0?(vt.test(f[2])&&(r=RegExp("</"+f[2],"g")),s=H):f[3]!==void 0&&(s=H):s===H?f[0]===">"?(s=r??Y,v=-1):f[1]===void 0?v=-2:(v=s.lastIndex-f[2].length,p=f[1],s=f[3]===void 0?H:f[3]==='"'?ht:ut):s===ht||s===ut?s=H:s===dt||s===ct?s=Y:(s=H,r=void 0);let w=s===H&&o[u+1].startsWith("/>")?" ":"";i+=s===Y?a+Wt:v>=0?(n.push(p),a.slice(0,v)+bt+a.slice(v)+P+w):a+P+(v===-2?u:w)}return[mt(o,i+(o[t]||"<?>")+(e===2?"</svg>":e===3?"</math>":"")),n]},Z=class o{constructor({strings:e,_$litType$:t},n){let r;this.parts=[];let i=0,s=0,u=e.length-1,a=this.parts,[p,f]=Jt(e,t);if(this.el=o.createElement(p,n),q.currentNode=this.el.content,t===2||t===3){let v=this.el.content.firstChild;v.replaceWith(...v.childNodes)}for(;(r=q.nextNode())!==null&&a.length<u;){if(r.nodeType===1){if(r.hasAttributes())for(let v of r.getAttributeNames())if(v.endsWith(bt)){let b=f[s++],w=r.getAttribute(v).split(P),$=/([.?@])?(.*)/.exec(b);a.push({type:1,index:i,name:$[2],strings:w,ctor:$[1]==="."?we:$[1]==="?"?$e:$[1]==="@"?ke:V}),r.removeAttribute(v)}else v.startsWith(P)&&(a.push({type:6,index:i}),r.removeAttribute(v));if(vt.test(r.tagName)){let v=r.textContent.split(P),b=v.length-1;if(b>0){r.textContent=de?de.emptyScript:"";for(let w=0;w<b;w++)r.append(v[w],G()),q.nextNode(),a.push({type:2,index:++i});r.append(v[b],G())}}}else if(r.nodeType===8)if(r.data===ft)a.push({type:2,index:i});else{let v=-1;for(;(v=r.data.indexOf(P,v+1))!==-1;)a.push({type:7,index:i}),v+=P.length-1}i++}}static createElement(e,t){let n=U.createElement("template");return n.innerHTML=e,n}};function F(o,e,t=o,n){if(e===B)return e;let r=n!==void 0?t._$Co?.[n]:t._$Cl,i=X(e)?void 0:e._$litDirective$;return r?.constructor!==i&&(r?._$AO?.(!1),i===void 0?r=void 0:(r=new i(o),r._$AT(o,t,n)),n!==void 0?(t._$Co??=[])[n]=r:t._$Cl=r),r!==void 0&&(e=F(o,r._$AS(o,e.values),r,n)),e}var xe=class{constructor(e,t){this._$AV=[],this._$AN=void 0,this._$AD=e,this._$AM=t}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(e){let{el:{content:t},parts:n}=this._$AD,r=(e?.creationScope??U).importNode(t,!0);q.currentNode=r;let i=q.nextNode(),s=0,u=0,a=n[0];for(;a!==void 0;){if(s===a.index){let p;a.type===2?p=new ee(i,i.nextSibling,this,e):a.type===1?p=new a.ctor(i,a.name,a.strings,this,e):a.type===6&&(p=new Ee(i,this,e)),this._$AV.push(p),a=n[++u]}s!==a?.index&&(i=q.nextNode(),s++)}return q.currentNode=U,r}p(e){let t=0;for(let n of this._$AV)n!==void 0&&(n.strings!==void 0?(n._$AI(e,n,t),t+=n.strings.length-2):n._$AI(e[t])),t++}},ee=class o{get _$AU(){return this._$AM?._$AU??this._$Cv}constructor(e,t,n,r){this.type=2,this._$AH=k,this._$AN=void 0,this._$AA=e,this._$AB=t,this._$AM=n,this.options=r,this._$Cv=r?.isConnected??!0}get parentNode(){let e=this._$AA.parentNode,t=this._$AM;return t!==void 0&&e?.nodeType===11&&(e=t.parentNode),e}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(e,t=this){e=F(this,e,t),X(e)?e===k||e==null||e===""?(this._$AH!==k&&this._$AR(),this._$AH=k):e!==this._$AH&&e!==B&&this._(e):e._$litType$!==void 0?this.$(e):e.nodeType!==void 0?this.T(e):Qt(e)?this.k(e):this._(e)}O(e){return this._$AA.parentNode.insertBefore(e,this._$AB)}T(e){this._$AH!==e&&(this._$AR(),this._$AH=this.O(e))}_(e){this._$AH!==k&&X(this._$AH)?this._$AA.nextSibling.data=e:this.T(U.createTextNode(e)),this._$AH=e}$(e){let{values:t,_$litType$:n}=e,r=typeof n=="number"?this._$AC(e):(n.el===void 0&&(n.el=Z.createElement(mt(n.h,n.h[0]),this.options)),n);if(this._$AH?._$AD===r)this._$AH.p(t);else{let i=new xe(r,this),s=i.u(this.options);i.p(t),this.T(s),this._$AH=i}}_$AC(e){let t=pt.get(e.strings);return t===void 0&&pt.set(e.strings,t=new Z(e)),t}k(e){Ae(this._$AH)||(this._$AH=[],this._$AR());let t=this._$AH,n,r=0;for(let i of e)r===t.length?t.push(n=new o(this.O(G()),this.O(G()),this,this.options)):n=t[r],n._$AI(i),r++;r<t.length&&(this._$AR(n&&n._$AB.nextSibling,r),t.length=r)}_$AR(e=this._$AA.nextSibling,t){for(this._$AP?.(!1,!0,t);e!==this._$AB;){let n=at(e).nextSibling;at(e).remove(),e=n}}setConnected(e){this._$AM===void 0&&(this._$Cv=e,this._$AP?.(e))}},V=class{get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}constructor(e,t,n,r,i){this.type=1,this._$AH=k,this._$AN=void 0,this.element=e,this.name=t,this._$AM=r,this.options=i,n.length>2||n[0]!==""||n[1]!==""?(this._$AH=Array(n.length-1).fill(new String),this.strings=n):this._$AH=k}_$AI(e,t=this,n,r){let i=this.strings,s=!1;if(i===void 0)e=F(this,e,t,0),s=!X(e)||e!==this._$AH&&e!==B,s&&(this._$AH=e);else{let u=e,a,p;for(e=i[0],a=0;a<i.length-1;a++)p=F(this,u[n+a],t,a),p===B&&(p=this._$AH[a]),s||=!X(p)||p!==this._$AH[a],p===k?e=k:e!==k&&(e+=(p??"")+i[a+1]),this._$AH[a]=p}s&&!r&&this.j(e)}j(e){e===k?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,e??"")}},we=class extends V{constructor(){super(...arguments),this.type=3}j(e){this.element[this.name]=e===k?void 0:e}},$e=class extends V{constructor(){super(...arguments),this.type=4}j(e){this.element.toggleAttribute(this.name,!!e&&e!==k)}},ke=class extends V{constructor(e,t,n,r,i){super(e,t,n,r,i),this.type=5}_$AI(e,t=this){if((e=F(this,e,t,0)??k)===B)return;let n=this._$AH,r=e===k&&n!==k||e.capture!==n.capture||e.once!==n.once||e.passive!==n.passive,i=e!==k&&(n===k||r);r&&this.element.removeEventListener(this.name,this,n),i&&this.element.addEventListener(this.name,this,e),this._$AH=e}handleEvent(e){typeof this._$AH=="function"?this._$AH.call(this.options?.host??this.element,e):this._$AH.handleEvent(e)}},Ee=class{constructor(e,t,n){this.element=e,this.type=6,this._$AN=void 0,this._$AM=t,this.options=n}get _$AU(){return this._$AM._$AU}_$AI(e){F(this,e)}};var Yt=Se.litHtmlPolyfillSupport;Yt?.(Z,ee),(Se.litHtmlVersions??=[]).push("3.3.2");var gt=(o,e,t)=>{let n=t?.renderBefore??e,r=n._$litPart$;if(r===void 0){let i=t?.renderBefore??null;n._$litPart$=r=new ee(e.insertBefore(G(),i),i,void 0,t??{})}return r._$AI(o),r};var ze=globalThis,z=class extends D{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){let e=super.createRenderRoot();return this.renderOptions.renderBefore??=e.firstChild,e}update(e){let t=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(e),this._$Do=gt(t,this.renderRoot,this.renderOptions)}connectedCallback(){super.connectedCallback(),this._$Do?.setConnected(!0)}disconnectedCallback(){super.disconnectedCallback(),this._$Do?.setConnected(!1)}render(){return B}};z._$litElement$=!0,z.finalized=!0,ze.litElementHydrateSupport?.({LitElement:z});var Gt=ze.litElementPolyfillSupport;Gt?.({LitElement:z});(ze.litElementVersions??=[]).push("4.2.2");var yt="bn-annotation-layer, bn-annotation-list, bn-body, bn-card, bn-chart-bubble, bn-chart-bullet, bn-chart-scatter, bn-chart-structure, bn-chart-time, bn-footer, bn-grid, bn-image, bn-layout-card, bn-layout-page, bn-message, bn-page, bn-table, bn-text, bn-title, bn-tree";function Le(){return document.querySelector("bn-context")}function ce(){var o=Le();return!!o&&typeof o.getLayoutState=="function"}function _t(o){return new Promise(function(e){setTimeout(e,o)})}function Xt(o){var e=[];return Array.prototype.forEach.call(o.querySelectorAll(yt),function(t){var n=t.getBoundingClientRect();e.push(Math.round(n.width)+"x"+Math.round(n.height))}),e.join(",")}var Zt=250,en=8e3;async function tn(o){for(var e=Date.now()+(o||en),t=Le();window.componentRegisterIsRenderedResult!==!0;){if(Date.now()>=e)return!1;await _t(100)}if(!t)return!0;for(var n=null;Date.now()<e;){var r=Xt(t);if(r===n)return!0;n=r,await _t(Zt)}return!1}function nn(o){var e={};return Array.prototype.map.call(o.querySelectorAll(yt),function(t){var n=t.localName,r=e[n]||0;e[n]=r+1;var i=t.getAttribute("id")||t.getAttribute("name");return{id:i||n+"["+r+"]",element:t}})}function rn(o){var e={};return o.forEach(function(t){var n=t.element,r=n.closest?n.closest("[data-bino-kind]"):null,i={};r&&(i.kind=r.getAttribute("data-bino-kind")||"",i.name=r.getAttribute("data-bino-name")||"",i.ref=r.getAttribute("data-bino-ref")||"");var s=n.getAttribute("measure-unit"),u=n.getAttribute("measure-scale");s&&(i.measureUnit=s),u&&(i.measureScale=u),(i.kind||i.name||i.ref||i.measureUnit)&&(e[t.id]=i)}),e}async function xt(o){var e=Le();if(!e||typeof e.getLayoutState!="function")return null;var t=await tn(o&&o.settleTimeoutMs),n=await e.getLayoutState(),r=nn(e),i=new Map;return r.forEach(function(s){i.set(s.id,s.element)}),{state:n,sources:rn(r),elements:i,settled:t}}async function wt(o){return!o||typeof o.getLayoutState!="function"?null:o.getLayoutState({detail:"full"})}var Te=class extends z{static properties={artifacts:{type:Array},documents:{type:Array},graph:{type:Object},currentPath:{type:String,attribute:"current-path"},_errorCount:{state:!0},_badgeVisible:{state:!0},_refreshing:{state:!0},_refreshError:{state:!0},_inspectorAvailable:{state:!0}};static styles=L`
    :host {
      position: fixed;
      top: 0;
      left: 0;
      right: 0;
      z-index: var(--bino-z-toolbar);
      display: flex;
      align-items: center;
      gap: var(--bino-space-md);
      background: var(--bino-surface);
      border-bottom: 1px solid var(--bino-border);
      padding: var(--bino-space-sm) var(--bino-space-md);
      font-size: var(--bino-font-size-md);
      font-family: var(--bino-font-sans);
      box-shadow: var(--bino-shadow-header);
    }
    .title {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-sm);
      font-weight: 600;
      color: var(--bino-text-muted);
    }
    .mark {
      height: 20px;
      width: auto;
      display: block;
    }
    select {
      padding: 0.375rem 0.625rem;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface-subtle);
      font-size: var(--bino-font-size-md);
      color: var(--bino-text-muted);
      cursor: pointer;
      min-width: var(--bino-search-width);
    }
    select:hover {
      border-color: var(--bino-border-hover);
    }
    select:focus {
      outline: none;
      border-color: var(--bino-primary);
      box-shadow: 0 0 0 3px var(--bino-focus-ring);
    }
    .warning-badge {
      display: none;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      background: var(--bino-warning-bg);
      border: 1px solid var(--bino-warning-border);
      color: var(--bino-warning-text);
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      cursor: pointer;
      user-select: none;
    }
    .warning-badge:hover {
      background: var(--bino-yellow-300);
    }
    .warning-badge.visible {
      display: inline-flex;
    }
    .warning-icon {
      font-size: var(--bino-font-size-md);
    }
    .assets-btn, .graph-btn, .explorer-btn, .inspect-btn, .build-btn {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      background: var(--bino-surface);
      border: 1px solid var(--bino-border-light);
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      font-family: var(--bino-font-sans);
      cursor: pointer;
      user-select: none;
    }
    .assets-btn:hover, .graph-btn:hover:not(:disabled), .explorer-btn:hover, .inspect-btn:hover:not(:disabled), .build-btn:hover:not(:disabled) {
      background: var(--bino-surface-hover);
      border-color: var(--bino-border-hover);
    }
    .doc-meta {
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-sm);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      max-width: 32ch;
    }
    .graph-btn:disabled, .present-btn:disabled, .inspect-btn:disabled, .build-btn:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
    .present-btn:disabled {
      background: var(--bino-surface);
      border-color: var(--bino-border-light);
      color: var(--bino-text-secondary);
    }
    .assets-icon, .graph-icon, .explorer-icon, .inspect-icon, .build-icon {
      font-size: var(--bino-font-size-md);
    }
    .present-btn {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      background: var(--bino-accent);
      border: 1px solid var(--bino-accent-strong);
      color: var(--bino-on-accent);
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      font-family: var(--bino-font-sans);
      cursor: pointer;
      user-select: none;
    }
    .present-btn:hover:not(:disabled) {
      background: var(--bino-accent-strong);
    }
    .present-icon {
      font-size: var(--bino-font-size-md);
    }
    .spacer {
      flex: 1;
    }
    ::slotted(*) {
      margin-left: auto;
    }
    .progress-bar {
      position: absolute;
      bottom: 0;
      left: 0;
      right: 0;
      height: 2px;
      overflow: hidden;
      opacity: 0;
      transition: opacity 0.15s ease;
    }
    .progress-bar.active {
      opacity: 1;
    }
    .progress-bar::after {
      content: '';
      display: block;
      height: 100%;
      width: 40%;
      background: var(--bino-accent);
      border-radius: 1px;
      animation: progress-slide 1.2s ease-in-out infinite;
    }
    .progress-bar.error {
      opacity: 1;
      height: 3px;
    }
    .progress-bar.error::after {
      width: 100%;
      background: var(--bino-error);
      animation: none;
    }
    @keyframes progress-slide {
      0% { transform: translateX(-100%); }
      100% { transform: translateX(350%); }
    }
    @media (prefers-reduced-motion: reduce) {
      .progress-bar::after {
        animation: none;
        width: 100%;
      }
    }
    .refresh-error-msg {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      background: var(--bino-error-bg);
      border: 1px solid var(--bino-error-border);
      color: var(--bino-error);
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      max-width: 32rem;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      cursor: help;
    }
  `;constructor(){super(),this.artifacts=[],this.documents=[],this.graph=null,this.currentPath="/",this._errorCount=0,this._badgeVisible=!1,this._refreshing=!1,this._refreshError="",this._panelDismissed=!1,this._inspectorAvailable=!1,this._boundOnContentUpdated=this._refreshInspectorAvailability.bind(this),this._boundOnErrorsChanged=this._onErrorsChanged.bind(this),this._boundOnPanelDismissed=this._onPanelDismissed.bind(this),this._boundOnRefreshing=this._onRefreshing.bind(this),this._boundOnRefreshDone=this._onRefreshDone.bind(this),this._boundOnRefreshError=this._onRefreshError.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-errors-changed",this._boundOnErrorsChanged),document.addEventListener("bino-panel-dismissed",this._boundOnPanelDismissed),document.addEventListener("bn-preview:refreshing",this._boundOnRefreshing),document.addEventListener("bn-preview:refresh-done",this._boundOnRefreshDone),document.addEventListener("bn-preview:refresh-error",this._boundOnRefreshError),document.addEventListener("bn-preview:content-updated",this._boundOnContentUpdated),this._refreshInspectorAvailability()}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-errors-changed",this._boundOnErrorsChanged),document.removeEventListener("bino-panel-dismissed",this._boundOnPanelDismissed),document.removeEventListener("bn-preview:refreshing",this._boundOnRefreshing),document.removeEventListener("bn-preview:refresh-done",this._boundOnRefreshDone),document.removeEventListener("bn-preview:refresh-error",this._boundOnRefreshError),document.removeEventListener("bn-preview:content-updated",this._boundOnContentUpdated)}render(){var e=this,t=this.currentPath||"/",n=this.artifacts||[],r=[],i=[];n.forEach(function(f){f.isDoc?i.push(f):r.push(f)});var s=t!=="/"&&!t.startsWith("/doc/")&&!t.startsWith("/pres/"),u=s?T()+"/pres"+t:null,a=i.find(function(f){return"/doc/"+f.name===t})||null,p=a||r.find(function(f){return"/"+f.name===t})||null;return c`
      <span class="title">
        <img class="mark" src=${T()+"/__bino/assets/bino-mark.png"} alt="">
        <span>bino preview</span>
      </span>
      <select id="artefact-select" @change=${this._onSelectChange}>
        <option value="/" ?selected=${t==="/"}>All Pages</option>
        ${r.length>0?c`
          <optgroup label="Report Artefacts">
            ${r.map(function(f){var v="/"+f.name,b=f.title?f.title+" ("+f.name+")":f.name;return c`<option value=${v} ?selected=${v===t}>${b}</option>`})}
          </optgroup>
        `:""}
        ${i.length>0?c`
          <optgroup label="Document Artefacts">
            ${i.map(function(f){var v="/doc/"+f.name,b=f.title?f.title+" ("+f.name+")":f.name;return c`<option value=${v} ?selected=${v===t}>${b}</option>`})}
          </optgroup>
        `:""}
      </select>
      ${a?c`
        <span class="doc-meta" title="Document settings from the manifest">${this._docMetaText(a)}</span>
      `:""}
      <span class="warning-badge ${this._badgeVisible?"visible":""}"
        title="Show warnings" @click=${this._onBadgeClick}>
        <span class="warning-icon">\u26A0</span>
        <span>${this._errorCount}</span>
      </span>
      <button class="assets-btn" title="Manifest documents" @click=${this._onAssetsClick}>
        <span class="assets-icon">\u25A6</span>
        <span>Assets (${(this.documents||[]).length})</span>
      </button>
      <button class="graph-btn" ?disabled=${!this.graph}
        title=${this.graph?"Dependency graph":"Dependency graph is only available for a single artefact"}
        @click=${this._onGraphClick}>
        <span class="graph-icon">\u229E</span>
        <span>Graph</span>
      </button>
      <button class="explorer-btn" title="Data Explorer" @click=${this._onExplorerClick}>
        <span class="explorer-icon">\u2636</span>
        <span>Explorer</span>
      </button>
      <button class="inspect-btn" ?disabled=${!this._inspectorAvailable}
        title=${this._inspectorAvailable?a?"Inspect embedded engine components":"Inspect the rendered report":"Requires template engine v1.0.0-next.24 or newer"}
        @click=${this._onInspectClick}>
        <span class="inspect-icon">\u25a3</span>
        <span>Inspect</span>
      </button>
      <button class="present-btn" ?disabled=${!u}
        title=${u?"Open presentation":"Presentation is only available for a report artefact"}
        @click=${function(){u&&window.open(u,"_blank")}}>
        <span class="present-icon">\u25B6</span>
        <span>Present</span>
      </button>
      <button class="build-btn" ?disabled=${!p}
        title=${p?"Build a preview PDF to view or download":"PDF is only available for a single artefact"}
        @click=${function(){e._onPdfClick(p)}}>
        <span class="build-icon">\u2193</span>
        <span>PDF</span>
      </button>
      <span class="spacer"></span>
      ${this._refreshError?c`
        <span class="refresh-error-msg" title=${this._refreshError}>
          <span>⚠</span>
          <span>Refresh failed</span>
        </span>
      `:""}
      <slot></slot>
      <div class="progress-bar ${this._refreshError?"error":this._refreshing?"active":""}"></div>
    `}updated(e){e.has("documents")&&document.dispatchEvent(new CustomEvent("bino-documents-changed",{detail:{documents:this.documents||[]}}))}_onAssetsClick(){document.dispatchEvent(new CustomEvent("bino-open-assets",{detail:{documents:this.documents||[]}}))}_onGraphClick(){document.dispatchEvent(new CustomEvent("bino-open-graph",{detail:{graph:this.graph}}))}_onExplorerClick(){document.dispatchEvent(new CustomEvent("bino-open-explorer"))}_onInspectClick(){document.dispatchEvent(new CustomEvent("bino-open-inspector"))}_docMetaText(e){var t=[];return e.format&&t.push(e.format),e.orientation&&t.push(e.orientation),e.locale&&t.push(e.locale),e.chapters===1&&t.push("1 chapter"),e.chapters>1&&t.push(e.chapters+" chapters"),e.toc&&t.push("TOC"),e.headerFooter&&t.push("header/footer"),t.join(" \xB7 ")}_onPdfClick(e){document.dispatchEvent(new CustomEvent("bino-open-pdf",{detail:{name:e.name,kind:e.isDoc?"DocumentArtefact":"ReportArtefact"}}))}_refreshInspectorAvailability(){this._inspectorAvailable=ce()}_onSelectChange(e){var t=e.target.value;t&&(window.location.href=T()+t)}_onBadgeClick(){this._panelDismissed=!1,this._badgeVisible=!1,document.dispatchEvent(new CustomEvent("bino-show-errors"))}_onErrorsChanged(e){this._errorCount=e.detail&&e.detail.count||0,this._panelDismissed&&this._errorCount>0?this._badgeVisible=!0:this._badgeVisible=!1}_onPanelDismissed(){this._panelDismissed=!0,this._errorCount>0&&(this._badgeVisible=!0)}_onRefreshing(){console.debug("bino-toolbar: refreshing \u2192 _refreshing=true"),this._refreshing=!0,this._refreshError=""}_onRefreshDone(){console.debug("bino-toolbar: refresh-done \u2192 _refreshing=false"),this._refreshing=!1}_onRefreshError(e){var t=e&&e.detail&&e.detail.message||"Refresh failed";console.debug("bino-toolbar: refresh-error",t),this._refreshError=String(t),this._refreshing=!1}};customElements.define("bino-toolbar",Te);var Me=class extends z{static properties={_errors:{state:!0},_visible:{state:!0}};static styles=L`
    :host {
      position: fixed;
      bottom: 0;
      left: 0;
      right: 0;
      max-height: var(--bino-panel-max-height);
      overflow-y: auto;
      background: var(--bino-warning-bg);
      border-top: 2px solid var(--bino-warning);
      font-family: var(--bino-font-sans);
      font-size: 13px;
      z-index: var(--bino-z-panel);
      box-shadow: var(--bino-shadow-panel);
      display: none;
    }
    :host([visible]) {
      display: block;
    }
    .header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 8px 12px;
      background: var(--bino-yellow-300);
      border-bottom: 1px solid var(--bino-warning-border);
      font-weight: 600;
      color: var(--bino-warning-text);
    }
    .close-btn {
      background: none;
      border: none;
      font-size: 18px;
      cursor: pointer;
      color: var(--bino-warning-text);
      padding: 0 4px;
    }
    .close-btn:hover {
      color: var(--bino-gray-900);
    }
    ul {
      list-style: none;
      margin: 0;
      padding: 0;
    }
    li {
      padding: 8px 12px;
      border-bottom: 1px solid var(--bino-yellow-300);
      cursor: pointer;
      display: flex;
      align-items: flex-start;
      gap: 8px;
    }
    li:hover {
      background: var(--bino-yellow-300);
    }
    li.highlighted {
      background: var(--bino-yellow-400);
      border-left: 3px solid var(--bino-warning);
    }
    li:last-child {
      border-bottom: none;
    }
    .badge {
      flex-shrink: 0;
      padding: 2px 6px;
      border-radius: 4px;
      font-size: var(--bino-font-size-xs);
      font-weight: 600;
      text-transform: uppercase;
    }
    .badge.warning {
      background: var(--bino-warning-border);
      color: var(--bino-gray-900);
    }
    .badge.error {
      background: var(--bino-error);
      color: #fff;
    }
    .message {
      color: var(--bino-warning-text);
    }
  `;constructor(){super(),this._errors=[],this._visible=!1,this._scanTimer=null,this._observer=null,this._badges=[],this._highlightTimer=null,this._boundOnShowErrors=this._onShowErrors.bind(this),this._boundOnContentUpdated=this._debouncedScan.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-show-errors",this._boundOnShowErrors),document.addEventListener("bn-preview:content-updated",this._boundOnContentUpdated),this._startObserver(),this._debouncedScan()}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-show-errors",this._boundOnShowErrors),document.removeEventListener("bn-preview:content-updated",this._boundOnContentUpdated),this._observer&&(this._observer.disconnect(),this._observer=null),this._removeBadges()}updated(e){e.has("_visible")&&(this._visible?this.setAttribute("visible",""):this.removeAttribute("visible"))}render(){if(!this._visible||this._errors.length===0)return c``;var e=this,t=this._errors.length;return c`
      <div class="header">
        <span>${t} warning${t!==1?"s":""} found</span>
        <button class="close-btn" title="Close" @click=${this._onClose}>&times;</button>
      </div>
      <ul>
        ${this._errors.map(function(n,r){return c`
            <li @click=${()=>e._scrollToElement(n.element)}>
              <span class="badge ${n.error.type||"warning"}">${n.error.type||"warning"}</span>
              <span class="message">${n.error.message||n.error.id||"Unknown error"}</span>
            </li>
          `})}
      </ul>
    `}_startObserver(){var e=this;this._observer=new MutationObserver(function(t){e._onMutation(t)}),this._observer.observe(document.body,{childList:!0,subtree:!0,attributes:!0,attributeFilter:["has-error","has-errors"]})}_onMutation(e){var t=!1;e.forEach(function(n){n.type==="attributes"&&(n.attributeName==="has-error"||n.attributeName==="has-errors")&&(t=!0),n.type==="childList"&&n.addedNodes.length>0&&n.addedNodes.forEach(function(r){r.nodeType===1&&r.hasAttribute&&(r.hasAttribute("has-error")||r.hasAttribute("has-errors"))&&(t=!0),r.nodeType===1&&r.querySelector&&r.querySelector("[has-error], [has-errors]")&&(t=!0)})}),t&&this._debouncedScan()}_debouncedScan(){var e=this;this._scanTimer&&clearTimeout(this._scanTimer),this._scanTimer=setTimeout(function(){e._scanForErrors()},100)}_parseErrors(e){if(!e)return[];try{var t=JSON.parse(e);return Array.isArray(t)?t:[]}catch{return[]}}_scanForErrors(){var e=[],t=document.querySelectorAll("[has-error], [has-errors]"),n=this;t.forEach(function(r){var i=r.getAttribute("has-error")||r.getAttribute("has-errors"),s=n._parseErrors(i);s.forEach(function(u){e.push({element:r,error:u})})}),this._errors=e,document.dispatchEvent(new CustomEvent("bino-errors-changed",{detail:{count:e.length}})),e.length>0?(this._visible=!0,this._injectBadges(e)):(this._visible=!1,this._removeBadges())}_highlightForElement(e){var t=this,n=this.renderRoot.querySelectorAll("li"),r=null;n.forEach(function(i,s){i.classList.remove("highlighted"),t._errors[s]&&t._errors[s].element===e&&(i.classList.add("highlighted"),r||(r=i))}),r&&r.scrollIntoView({block:"nearest",behavior:"smooth"}),this._highlightTimer&&clearTimeout(this._highlightTimer),this._highlightTimer=setTimeout(function(){n.forEach(function(i){i.classList.remove("highlighted")})},4e3)}_onClose(){this._visible=!1,document.dispatchEvent(new CustomEvent("bino-panel-dismissed"))}_onShowErrors(){this._errors.length>0&&(this._visible=!0)}_scrollToElement(e){e&&(e.scrollIntoView({behavior:"smooth",block:"center"}),e.classList.remove("bn-error-highlight"),e.offsetWidth,e.classList.add("bn-error-highlight"),setTimeout(function(){e.classList.remove("bn-error-highlight")},700))}_injectBadges(e){this._removeBadges();var t=this,n=new Map;e.forEach(function(r,i){n.has(r.element)||n.set(r.element,[]),n.get(r.element).push({error:r.error,index:i})}),n.forEach(function(r,i){var s=document.createElement("div");s.className="bn-error-indicator-badge",s.style.cssText="position:absolute;top:2px;right:2px;width:18px;height:18px;background:#fbc02d;color:#11161a;font-size:12px;border-radius:50%;display:flex;align-items:center;justify-content:center;z-index:10000;cursor:pointer;user-select:none;line-height:1;",s.textContent="\u26A0",s.title=r.map(function(p){return p.error.message||p.error.id||"Error"}).join(`
`),s.addEventListener("click",function(p){p.stopPropagation(),t._visible=!0,t._highlightForElement(i)});var u=i.parentNode;if(u){var a=window.getComputedStyle(u);a.position==="static"&&(u.style.position="relative"),i.insertAdjacentElement("afterend",s)}t._badges.push(s)})}_removeBadges(){this._badges.forEach(function(e){e.parentNode&&e.parentNode.removeChild(e)}),this._badges=[]}};customElements.define("bino-error-panel",Me);var Re=class extends z{static properties={_results:{state:!0},_activeIndex:{state:!0},_open:{state:!0}};static styles=L`
    :host {
      position: relative;
      display: inline-block;
      font-family: var(--bino-font-sans);
    }
    .search-wrap {
      position: relative;
      display: flex;
      align-items: center;
    }
    .search-icon {
      position: absolute;
      left: var(--bino-space-sm);
      pointer-events: none;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
      line-height: 1;
    }
    input {
      width: var(--bino-search-width);
      padding: 0.375rem 0.625rem 0.375rem 1.75rem;
      border: 1px solid var(--bino-border-light);
      border-radius: var(--bino-radius);
      font-size: var(--bino-font-size-base);
      font-family: inherit;
      color: var(--bino-text);
      background: var(--bino-surface-subtle);
      transition: width var(--bino-transition-normal), border-color var(--bino-transition-fast), box-shadow var(--bino-transition-fast);
    }
    input:focus {
      width: var(--bino-search-width-focus);
      outline: none;
      border-color: var(--bino-primary);
      box-shadow: 0 0 0 3px var(--bino-focus-ring);
      background: var(--bino-surface);
    }
    input::placeholder {
      color: var(--bino-text-secondary);
    }
    .dropdown {
      display: none;
      position: absolute;
      top: 100%;
      right: 0;
      margin-top: 4px;
      min-width: 320px;
      max-height: 400px;
      overflow-y: auto;
      background: var(--bino-surface);
      border: 1px solid var(--bino-border);
      border-radius: var(--bino-radius);
      box-shadow: var(--bino-shadow-dropdown);
      z-index: var(--bino-z-panel);
    }
    .dropdown.open {
      display: block;
    }
    .result {
      display: flex;
      flex-direction: column;
      gap: 2px;
      padding: var(--bino-space-sm) 0.75rem;
      cursor: pointer;
      border-bottom: 1px solid var(--bino-surface-hover);
      font-size: var(--bino-font-size-base);
      transition: background 0.1s;
    }
    .result:last-child {
      border-bottom: none;
    }
    .result:hover, .result.active {
      background: var(--bino-surface-active);
    }
    .result-name {
      font-weight: 500;
      color: var(--bino-text);
    }
    .result-kind {
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      text-transform: uppercase;
      letter-spacing: 0.03em;
    }
    .result-context {
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text-secondary);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .no-results {
      padding: 0.75rem;
      text-align: center;
      font-size: var(--bino-font-size-base);
      color: var(--bino-text-secondary);
    }
    mark {
      background: var(--bino-yellow-400);
      color: inherit;
      border-radius: 2px;
      padding: 0 1px;
    }
  `;constructor(){super(),this._results=[],this._activeIndex=-1,this._open=!1,this._debounceTimer=null,this._query=""}connectedCallback(){super.connectedCallback();var e=this;this._boundOutsideClick=function(t){!e.contains(t.target)&&!e.renderRoot.contains(t.target)&&e._close()},document.addEventListener("click",this._boundOutsideClick),this._boundContentUpdated=function(){e._query&&e._search(e._query)},document.addEventListener("bn-preview:content-updated",this._boundContentUpdated)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("click",this._boundOutsideClick),document.removeEventListener("bn-preview:content-updated",this._boundContentUpdated)}render(){var e=this,t=ie().startsWith("/doc/")?"Search headings...":"Search elements...";return c`
      <div class="search-wrap">
        <span class="search-icon">\u2315</span>
        <input type="text" placeholder=${t} autocomplete="off" spellcheck="false"
          @input=${this._onInput}
          @keydown=${this._onKeydown}
          @focus=${this._onFocus}>
      </div>
      <div class="dropdown ${this._open?"open":""}">
        ${this._results.length===0&&this._open?c`<div class="no-results">No results found</div>`:this._results.map(function(n,r){return c`
                <div class="result ${r===e._activeIndex?"active":""}"
                  @click=${()=>e._selectResult(r)}>
                  <span class="result-kind">${n.kind}</span>
                  <span class="result-name">${e._highlightMatch(n,e._query)}</span>
                </div>
              `})}
      </div>
    `}_highlightMatch(e,t){if(!t)return e.name;var n=t.toLowerCase(),r=e.name.toLowerCase().indexOf(n);if(r===-1)return e.name;var i=e.name.substring(0,r),s=e.name.substring(r,r+t.length),u=e.name.substring(r+t.length);return c`${i}<mark>${s}</mark>${u}`}_onInput(e){var t=this,n=e.target.value.trim();clearTimeout(this._debounceTimer),this._debounceTimer=setTimeout(function(){t._query=n,t._search(n)},150)}_onKeydown(e){if(e.key==="Escape"){this._close(),e.target.blur();return}if(e.key==="ArrowDown"){e.preventDefault(),this._moveActive(1);return}if(e.key==="ArrowUp"){e.preventDefault(),this._moveActive(-1);return}if(e.key==="Enter"){e.preventDefault(),this._activeIndex>=0&&this._activeIndex<this._results.length?this._selectResult(this._activeIndex):this._results.length>0&&this._selectResult(0);return}}_onFocus(){this._query&&this._results.length>0&&(this._open=!0)}_search(e){if(this._activeIndex=-1,!e||e.length<2){this._results=[],this._close();return}var t=e.toLowerCase(),n=[],r=new Set,i=document.querySelectorAll("[data-bino-kind]");i.forEach(function(a){var p=a.getAttribute("data-bino-kind")||"",f=a.getAttribute("data-bino-name")||"",v=p+":"+f;r.has(v)||(p.toLowerCase().indexOf(t)!==-1||f.toLowerCase().indexOf(t)!==-1)&&(r.add(v),n.push({type:"element",kind:p,name:f,el:a}))});var s=document.querySelectorAll("bn-layout-page[data-bino-page]");s.forEach(function(a){var p=a.getAttribute("data-bino-page")||"",f="page:"+p;r.has(f)||p.toLowerCase().indexOf(t)!==-1&&(r.add(f),n.push({type:"page",kind:"LayoutPage",name:p,el:a}))});var u=document.querySelectorAll(".bn-document-content h1, .bn-document-content h2, .bn-document-content h3, .bn-document-content h4, .bn-document-content h5, .bn-document-content h6");u.forEach(function(a){var p=(a.textContent||"").trim();if(p){var f="heading:"+(a.id||p);r.has(f)||p.toLowerCase().indexOf(t)!==-1&&(r.add(f),n.push({type:"heading",kind:a.tagName.toLowerCase()+" heading",name:p,el:a}))}}),n.length<50&&s.forEach(function(a){for(var p=a.getAttribute("data-bino-page")||"",f=a.shadowRoot||a,v=f.querySelectorAll("*"),b=0;b<v.length&&n.length<50;b++){var w=v[b];if(!(w.tagName==="SCRIPT"||w.tagName==="STYLE"))for(var $=w.childNodes,m=0;m<$.length;m++){var S=$[m];if(S.nodeType===3){var x=S.textContent.trim();if(!(!x||x.length<2)){var l=x.toLowerCase().indexOf(t);if(l!==-1){var y=x.substring(Math.max(0,l-30),Math.min(x.length,l+e.length+30)),d="text:"+p+":"+x.substring(l,l+Math.min(40,x.length-l));if(r.has(d))continue;r.add(d),n.push({type:"text",kind:"text in "+p,name:y,el:w,query:e});break}}}}}}),this._results=n,this._open=!0}_moveActive(e){if(this._results.length!==0){var t=this._activeIndex+e;t<0&&(t=this._results.length-1),t>=this._results.length&&(t=0),this._activeIndex=t,this.updateComplete.then(()=>{var n=this.renderRoot.querySelectorAll(".result");n[t]&&n[t].scrollIntoView({block:"nearest"})})}}_selectResult(e){var t=this._results[e];if(!(!t||!t.el)){this._close(),t.el.scrollIntoView({behavior:"smooth",block:"center"});var n=t.el,r=n.style.outline,i=n.style.outlineOffset;n.style.outline="2px solid "+(getComputedStyle(document.documentElement).getPropertyValue("--bino-primary").trim()||"#0b727e"),n.style.outlineOffset="2px",setTimeout(function(){n.style.outline=r,n.style.outlineOffset=i},3e3)}}_close(){this._open=!1,this._activeIndex=-1}};customElements.define("bino-search",Re);var De=class extends z{static properties={_documents:{state:!0},_open:{state:!0},_selectedDoc:{state:!0},_filterKind:{state:!0}};static styles=L`
    :host {
      font-family: var(--bino-font-sans);
    }
    .backdrop {
      position: fixed;
      inset: 0;
      background: var(--bino-scrim);
      z-index: var(--bino-z-modal);
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .modal {
      background: var(--bino-surface);
      border-radius: var(--bino-radius-lg);
      box-shadow: var(--bino-shadow-page);
      width: 640px;
      max-width: calc(100vw - 2rem);
      max-height: 80vh;
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }
    .modal-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: var(--bino-space-md) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .modal-header h2 {
      margin: 0;
      font-size: var(--bino-font-size-md);
      font-weight: 600;
      color: var(--bino-text);
    }
    .close-btn {
      background: none;
      border: none;
      font-size: 20px;
      cursor: pointer;
      color: var(--bino-text-secondary);
      padding: 0 4px;
      line-height: 1;
    }
    .close-btn:hover {
      color: var(--bino-text);
    }
    .kind-tabs {
      display: flex;
      gap: var(--bino-space-xs);
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-wrap: wrap;
      flex-shrink: 0;
    }
    .kind-tab {
      padding: var(--bino-space-xs) 0.625rem;
      border-radius: 999px;
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface);
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text-secondary);
      cursor: pointer;
      font-family: var(--bino-font-sans);
      user-select: none;
    }
    .kind-tab:hover {
      background: var(--bino-surface-hover);
    }
    .kind-tab.active {
      background: var(--bino-surface-active);
      border-color: var(--bino-primary);
      color: var(--bino-active-text);
      font-weight: 600;
    }
    .doc-list {
      flex: 1;
      overflow-y: auto;
      min-height: 0;
    }
    .doc-row {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      cursor: pointer;
    }
    .doc-row:last-child {
      border-bottom: none;
    }
    .doc-row:hover {
      background: var(--bino-surface-hover);
    }
    .doc-row.selected {
      background: var(--bino-surface-active);
    }
    .kind-badge {
      flex-shrink: 0;
      padding: 2px 6px;
      border-radius: 4px;
      font-size: var(--bino-font-size-xs);
      font-weight: 600;
      text-transform: uppercase;
      background: var(--bino-celeste-100);
      color: var(--bino-celeste-800);
      white-space: nowrap;
    }
    .doc-info {
      min-width: 0;
      flex: 1;
    }
    .doc-name {
      font-weight: 600;
      font-size: var(--bino-font-size-base);
      color: var(--bino-text);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .doc-file {
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .detail-panel {
      border-top: 2px solid var(--bino-border);
      padding: var(--bino-space-md) var(--bino-space-lg);
      flex-shrink: 0;
      background: var(--bino-surface-subtle);
    }
    .detail-row {
      display: flex;
      gap: var(--bino-space-sm);
      margin-bottom: var(--bino-space-xs);
      font-size: var(--bino-font-size-sm);
      align-items: baseline;
    }
    .detail-row:last-child {
      margin-bottom: 0;
    }
    .detail-label {
      color: var(--bino-text-secondary);
      font-weight: 600;
      flex-shrink: 0;
      min-width: 80px;
    }
    .detail-value {
      color: var(--bino-text);
      min-width: 0;
      word-break: break-word;
    }
    .pills {
      display: flex;
      flex-wrap: wrap;
      gap: var(--bino-space-xs);
    }
    .pill {
      padding: 1px 6px;
      border-radius: 4px;
      font-size: var(--bino-font-size-xs);
      background: var(--bino-gray-200);
      color: var(--bino-text-muted);
    }
    .pill.label-pill {
      background: var(--bino-gray-100);
      color: var(--bino-gray-700);
    }
    .pill.constraint-pill {
      background: var(--bino-yellow-200);
      color: var(--bino-gray-800);
    }
    .empty {
      padding: var(--bino-space-xl);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
    }
  `;constructor(){super(),this._documents=[],this._open=!1,this._selectedDoc=null,this._filterKind="",this._boundOnOpen=this._onOpen.bind(this),this._boundOnChanged=this._onChanged.bind(this),this._boundOnKeydown=this._onKeydown.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-open-assets",this._boundOnOpen),document.addEventListener("bino-documents-changed",this._boundOnChanged),document.addEventListener("keydown",this._boundOnKeydown)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-open-assets",this._boundOnOpen),document.removeEventListener("bino-documents-changed",this._boundOnChanged),document.removeEventListener("keydown",this._boundOnKeydown)}render(){if(!this._open)return k;var e=this,t=this._filteredDocs(),n=this._uniqueKinds();return c`
      <div class="backdrop" @click=${this._onBackdropClick}>
        <div class="modal" @click=${this._stopPropagation}>
          <div class="modal-header">
            <h2>Manifest Documents</h2>
            <button class="close-btn" title="Close" @click=${this._close}>&times;</button>
          </div>
          <div class="kind-tabs">
            <button class="kind-tab ${this._filterKind===""?"active":""}"
              @click=${function(){e._filterKind="",e._selectedDoc=null}}>
              All (${this._documents.length})
            </button>
            ${n.map(function(r){var i=e._countKind(r);return c`
                <button class="kind-tab ${e._filterKind===r?"active":""}"
                  @click=${function(){e._filterKind=r,e._selectedDoc=null}}>
                  ${r} (${i})
                </button>
              `})}
          </div>
          <div class="doc-list">
            ${t.length===0?c`<div class="empty">No documents found</div>`:t.map(function(r){var i=e._selectedDoc===r;return c`
                    <div class="doc-row ${i?"selected":""}"
                      @click=${function(){e._selectedDoc=i?null:r}}>
                      <span class="kind-badge">${r.kind}</span>
                      <div class="doc-info">
                        <div class="doc-name">${r.name}</div>
                        <div class="doc-file">${r.file}</div>
                      </div>
                    </div>
                  `})}
          </div>
          ${this._selectedDoc?this._renderDetail(this._selectedDoc):k}
        </div>
      </div>
    `}_renderDetail(e){var t=e.labels||{},n=Object.keys(t),r=e.constraints||[];return c`
      <div class="detail-panel">
        <div class="detail-row">
          <span class="detail-label">Kind</span>
          <span class="detail-value">${e.kind}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">Name</span>
          <span class="detail-value">${e.name}</span>
        </div>
        <div class="detail-row">
          <span class="detail-label">File</span>
          <span class="detail-value">${e.file}</span>
        </div>
        ${n.length>0?c`
          <div class="detail-row">
            <span class="detail-label">Labels</span>
            <div class="pills">
              ${n.map(function(i){return c`<span class="pill label-pill">${i}: ${t[i]}</span>`})}
            </div>
          </div>
        `:k}
        ${r.length>0?c`
          <div class="detail-row">
            <span class="detail-label">Constraints</span>
            <div class="pills">
              ${r.map(function(i){return c`<span class="pill constraint-pill">${i}</span>`})}
            </div>
          </div>
        `:k}
      </div>
    `}_filteredDocs(){if(!this._filterKind)return this._documents;var e=this._filterKind;return this._documents.filter(function(t){return t.kind===e})}_uniqueKinds(){var e={},t=[];return this._documents.forEach(function(n){e[n.kind]||(e[n.kind]=!0,t.push(n.kind))}),t}_countKind(e){var t=0;return this._documents.forEach(function(n){n.kind===e&&t++}),t}_onOpen(e){this._documents=e.detail&&e.detail.documents||[],this._open=!0,this._selectedDoc=null,this._filterKind=""}_onChanged(e){if(this._open&&(this._documents=e.detail&&e.detail.documents||[],this._selectedDoc)){var t=this._selectedDoc,n=this._documents.some(function(r){return r.kind===t.kind&&r.name===t.name&&r.file===t.file});n||(this._selectedDoc=null)}}_onKeydown(e){this._open&&e.key==="Escape"&&this._close()}_onBackdropClick(){this._close()}_stopPropagation(e){e.stopPropagation()}_close(){this._open=!1,this._selectedDoc=null}};customElements.define("bino-assets-modal",De);var K=170,te=38,Ne=20,on=56,ue=30,sn={ReportArtefact:{bg:"#d4f7f9",stroke:"#0b727e",text:"#0c454c"},DocumentArtefact:{bg:"#d4f7f9",stroke:"#0b727e",text:"#0c454c"},LayoutPage:{bg:"#d6ecdd",stroke:"#1f7a3d",text:"#1f7a3d"},LayoutCard:{bg:"#ecfcfd",stroke:"#5cdae5",text:"#0c5a64"},Component:{bg:"#f7aace",stroke:"#e23e8c",text:"#c11f6e"},DataSet:{bg:"#fff0a8",stroke:"#d99e0b",text:"#1f262a"},DataSource:{bg:"#f6ddda",stroke:"#c0392b",text:"#c0392b"},MarkdownFile:{bg:"#eef2f3",stroke:"#9aa7ad",text:"#333c41"}},an={ReportArtefact:"Artefact",DocumentArtefact:"DocArtefact",LayoutPage:"Page",LayoutCard:"Card",Component:"Component",DataSet:"DataSet",DataSource:"Source",MarkdownFile:"Markdown"};function $t(o){return sn[o]||{bg:"#eef2f3",stroke:"#9aa7ad",text:"#333c41"}}function ln(o){return an[o]||o}function dn(o,e){return e||(e=20),!o||o.length<=e?o||"":o.substring(0,e-1)+"\u2026"}function cn(o){return o==="auto"||o.indexOf("inherited-")===0||o.indexOf("${")===0}function un(o){for(var e={},t=0,n=0;n<o.length;n++)e[o[n].column]||(e[o[n].column]=!0,t++);return t}function hn(o){for(var e=[],t=null,n=null,r=0;r<o.length;r++){var i=o[r];(!t||t.name!==i.dataset)&&(t={name:i.dataset,groups:[]},e.push(t),n=null),(!n||n.role!==i.role)&&(n={role:i.role,columns:[]},t.groups.push(n)),n.columns.indexOf(i.column)===-1&&n.columns.push(i.column)}return e}function pn(o){if(!o||!o.rootId)return null;var e=o.nodes||{},t={},n={};function r(i){if(!i)return null;var s=e[i];if(!s)return null;if(t[i])return{id:i,kind:s.kind,name:s.name||i,children:[],cycle:!0};if(n[i])return{id:i,kind:s.kind,name:s.name||i,children:[],ref:!0};t[i]=!0,n[i]=!0;for(var u=[],a=s.dependsOn||[],p=0;p<a.length;p++){var f=r(a[p]);f&&u.push(f)}return t[i]=!1,{id:i,kind:s.kind,name:s.name||i,children:u}}return r(o.rootId)}function kt(o){if(!o)return 0;if(o.children.length===0)return o.w=K,K;for(var e=0,t=0;t<o.children.length;t++)e+=kt(o.children[t]);return e+=(o.children.length-1)*Ne,o.w=Math.max(e,K),o.w}function bn(o){if(!o)return{nodes:[],edges:[],width:0,height:0};kt(o);var e=[],t=[],n=0,r=0;function i(s,u,a){if(s){var p=u-K/2;if(e.push({id:s.id,kind:s.kind,name:s.name,x:p,y:a,cx:u,ref:s.ref||!1,cycle:s.cycle||!1}),u+K/2>n&&(n=u+K/2),a+te>r&&(r=a+te),s.children.length!==0){for(var f=a+te+on,v=0,b=0;b<s.children.length;b++)v+=s.children[b].w;v+=(s.children.length-1)*Ne;for(var w=u-v/2,b=0;b<s.children.length;b++){var $=s.children[b],m=w+$.w/2;t.push({x1:u,y1:a+te,x2:m,y2:f}),i($,m,f),w+=$.w+Ne}}}}return i(o,o.w/2+ue,ue),{nodes:e,edges:t,width:n+ue,height:r+ue}}var Pe=class extends z{static properties={_graphData:{state:!0},_open:{state:!0},_selected:{state:!0}};static styles=L`
    :host { font-family: var(--bino-font-sans); }
    .backdrop {
      position: fixed;
      inset: 0;
      background: var(--bino-scrim);
      z-index: var(--bino-z-modal);
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .modal {
      background: var(--bino-surface);
      border-radius: var(--bino-radius-lg);
      box-shadow: var(--bino-shadow-page);
      width: 90vw;
      max-width: 1200px;
      max-height: 85vh;
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }
    .modal-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: var(--bino-space-md) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .modal-header h2 {
      margin: 0;
      font-size: var(--bino-font-size-md);
      font-weight: 600;
      color: var(--bino-text);
    }
    .close-btn {
      background: none;
      border: none;
      font-size: 20px;
      cursor: pointer;
      color: var(--bino-text-secondary);
      padding: 0 4px;
      line-height: 1;
    }
    .close-btn:hover { color: var(--bino-text); }
    .graph-container {
      flex: 1;
      overflow: auto;
      min-height: 0;
      background: var(--bino-surface-subtle);
      padding: var(--bino-space-md);
    }
    svg { display: block; }
    svg text { font-family: var(--bino-font-sans); }
    .legend {
      display: flex;
      gap: var(--bino-space-md);
      flex-wrap: wrap;
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-top: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .legend-item {
      display: flex;
      align-items: center;
      gap: var(--bino-space-xs);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
    }
    .legend-swatch {
      width: 12px;
      height: 12px;
      border-radius: 3px;
    }
    .columns {
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-top: 1px solid var(--bino-border);
      flex-shrink: 0;
      max-height: 30vh;
      overflow: auto;
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text);
    }
    .columns-title {
      font-weight: 600;
      margin-bottom: var(--bino-space-xs);
    }
    .columns-row {
      display: flex;
      flex-wrap: wrap;
      gap: var(--bino-space-xs) var(--bino-space-md);
    }
    .columns-dataset { font-weight: 600; }
    .columns-role { color: var(--bino-text-secondary); }
    .columns .muted { color: var(--bino-text-muted); font-style: italic; }
    .empty {
      padding: var(--bino-space-xl);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
    }
  `;constructor(){super(),this._graphData=null,this._open=!1,this._selected=null,this._boundOnOpen=this._onOpen.bind(this),this._boundOnKeydown=this._onKeydown.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-open-graph",this._boundOnOpen),document.addEventListener("keydown",this._boundOnKeydown)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-open-graph",this._boundOnOpen),document.removeEventListener("keydown",this._boundOnKeydown)}render(){if(!this._open)return k;var e=pn(this._graphData),t=bn(e);return c`
      <div class='backdrop' @click=${this._close}>
        <div class='modal' @click=${this._stop}>
          <div class='modal-header'>
            <h2>Dependency Graph</h2>
            <button class='close-btn' title='Close' @click=${this._close}>&times;</button>
          </div>
          <div class='graph-container'>
            ${this._renderSVG(t)}
          </div>
          ${this._renderColumns()}
          ${this._renderLegend(t)}
        </div>
      </div>
    `}_renderSVG(e){if(!e||e.nodes.length===0)return c`<div class='empty'>No graph data available</div>`;var t=this,n=e.width,r=e.height,i=this._graphData&&this._graphData.nodes||{},s=e.edges.map(function(a){var p=(a.y1+a.y2)/2;return Oe`<path d=${"M"+a.x1+","+a.y1+" C"+a.x1+","+p+" "+a.x2+","+p+" "+a.x2+","+a.y2}
        fill='none' stroke='#cbd4d8' stroke-width='1.5'/>`}),u=e.nodes.map(function(a){var p=$t(a.kind),f=a.ref||a.cycle?"0.5":"1",v=i[a.id]&&i[a.id].columns||[],b=ln(a.kind);if(v.length>0){var w=un(v);b+=" \xB7 "+w+(w===1?" col":" cols")}var $=dn(a.name),m=a.cycle?" [cycle]":a.ref?" [ref]":"";return Oe`
        <g opacity=${f} style=${v.length>0?"cursor: pointer":k}
          @click=${v.length>0?function(){t._selected=a.id}:k}>
          <rect x=${a.x} y=${a.y} width=${K} height=${te}
            rx='6' fill=${p.bg} stroke=${p.stroke} stroke-width=${a.id===t._selected?"3":"1.5"}/>
          <text x=${a.cx} y=${a.y+14} text-anchor='middle'
            font-size='10' font-weight='600' fill=${p.stroke}>${b}</text>
          <text x=${a.cx} y=${a.y+28} text-anchor='middle'
            font-size='11' fill=${p.text}>${$}${m}</text>
        </g>
      `});return c`
      <svg width=${n} height=${r} viewBox=${"0 0 "+n+" "+r}>
        ${s}
        ${u}
      </svg>
    `}_renderColumns(){var e=this._graphData&&this._graphData.nodes||{},t=e[this._selected];return!t||!t.columns?k:c`
      <div class='columns'>
        <div class='columns-title'>${t.name||t.id}</div>
        ${hn(t.columns).map(function(n){return c`
            <div class='columns-row'>
              <span class='columns-dataset'>${n.name}</span>
              ${n.groups.map(function(r){return c`<span><span class='columns-role'>${r.role}:</span> ${r.columns.map(function(i,s){return c`${s>0?", ":""}<span class=${cn(i)?"muted":""}>${i}</span>`})}</span>`})}
            </div>
          `})}
      </div>
    `}_renderLegend(e){if(!e||e.nodes.length===0)return k;var t={},n=[];return e.nodes.forEach(function(r){t[r.kind]||(t[r.kind]=!0,n.push(r.kind))}),n.length===0?k:c`
      <div class='legend'>
        ${n.map(function(r){var i=$t(r);return c`
            <span class='legend-item'>
              <span class='legend-swatch' style=${"background:"+i.bg+";border:1.5px solid "+i.stroke}></span>
              ${r}
            </span>
          `})}
      </div>
    `}_onOpen(e){this._graphData=e.detail&&e.detail.graph||null,this._selected=null,this._open=!0}_onKeydown(e){this._open&&e.key==="Escape"&&this._close()}_close(){this._open=!1}_stop(e){e.stopPropagation()}};customElements.define("bino-graph-modal",Pe);function fn(){var o=window.location.ancestorOrigins;if(!o)return!1;for(var e=0;e<o.length;e++)if(o[e].indexOf("vscode-webview:")===0)return!0;return!1}function vn(o){for(var e=atob(o),t=new Uint8Array(e.length),n=0;n<e.length;n++)t[n]=e.charCodeAt(n);return new Blob([t],{type:"application/pdf"})}var Ie=class extends z{static properties={_open:{state:!0},_state:{state:!0},_url:{state:!0},_error:{state:!0},_warnings:{state:!0}};static styles=L`
    :host { font-family: var(--bino-font-sans); }
    .backdrop {
      position: fixed;
      inset: 0;
      background: var(--bino-scrim);
      z-index: var(--bino-z-modal);
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .modal {
      background: var(--bino-surface);
      border-radius: var(--bino-radius-lg);
      box-shadow: var(--bino-shadow-page);
      width: 90vw;
      max-width: 1200px;
      height: 85vh;
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }
    .modal-header {
      display: flex;
      align-items: center;
      gap: var(--bino-space-md);
      padding: var(--bino-space-md) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .modal-header h2 {
      flex: 1;
      margin: 0;
      font-size: var(--bino-font-size-md);
      font-weight: 600;
      color: var(--bino-text);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .download-btn {
      padding: 4px 12px;
      border-radius: var(--bino-radius);
      background: var(--bino-accent);
      border: 1px solid var(--bino-accent-strong);
      color: var(--bino-on-accent);
      font-size: var(--bino-font-size-sm);
      font-weight: 600;
      text-decoration: none;
    }
    .download-btn:hover { background: var(--bino-accent-strong); }
    .close-btn {
      background: none;
      border: none;
      font-size: 20px;
      cursor: pointer;
      color: var(--bino-text-secondary);
      padding: 0 4px;
      line-height: 1;
    }
    .close-btn:hover { color: var(--bino-text); }
    .warnings {
      flex-shrink: 0;
      max-height: 30%;
      overflow: auto;
      padding: var(--bino-space-sm) var(--bino-space-lg);
      background: var(--bino-warning-bg);
      border-bottom: 1px solid var(--bino-warning-border);
      color: var(--bino-warning-text);
      font-size: var(--bino-font-size-sm);
    }
    .warnings summary { cursor: pointer; font-weight: 600; }
    .warnings li { white-space: pre-wrap; overflow-wrap: anywhere; }
    .viewer {
      flex: 1;
      min-height: 0;
      border: 0;
      background: var(--bino-surface-subtle);
    }
    .message {
      flex: 1;
      min-height: 0;
      overflow: auto;
      padding: var(--bino-space-xl);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
    }
    .message a { color: var(--bino-primary); overflow-wrap: anywhere; }
    .error {
      margin: 0;
      padding: var(--bino-space-md);
      border: 1px solid var(--bino-error-border);
      border-radius: var(--bino-radius);
      background: var(--bino-error-bg);
      color: var(--bino-text);
      font: var(--bino-font-size-sm) var(--bino-font-mono);
      text-align: left;
      white-space: pre-wrap;
      overflow-wrap: anywhere;
    }
    .footer {
      flex-shrink: 0;
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-top: 1px solid var(--bino-border);
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-xs);
    }
  `;constructor(){super(),this._open=!1,this._state="building",this._url="",this._error="",this._warnings=[],this._name="",this._controller=null,this._boundOnOpen=this._onOpen.bind(this),this._boundOnKeydown=this._onKeydown.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-open-pdf",this._boundOnOpen),document.addEventListener("keydown",this._boundOnKeydown)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-open-pdf",this._boundOnOpen),document.removeEventListener("keydown",this._boundOnKeydown),this._reset()}render(){return this._open?c`
      <div class='backdrop' @click=${this._close}>
        <div class='modal' @click=${this._stop}>
          <div class='modal-header'>
            <h2>PDF preview: ${this._name}</h2>
            ${this._state==="ready"?c`
              <a class='download-btn' href=${this._url}
                download=${this._name.replace(/[@/]/g,"_")+"-preview.pdf"}>Download</a>
            `:""}
            <button class='close-btn' title='Close' @click=${this._close}>&times;</button>
          </div>
          ${this._renderWarnings()}
          ${this._renderBody()}
          ${this._state==="ready"?c`
            <div class='footer'>
              Every page is marked PREVIEW. For the final PDF run: bino build --artefact ${this._name}
            </div>
          `:""}
        </div>
      </div>
    `:k}_renderWarnings(){var e=this._warnings.length;return e===0?"":c`
      <details class='warnings' ?open=${this._state==="error"}>
        <summary>${e} warning${e!==1?"s":""}. The PDF may be incomplete.</summary>
        <ul>${this._warnings.map(function(t){return c`<li>${t}</li>`})}</ul>
      </details>
    `}_renderBody(){return this._state==="embedded"?c`
        <div class='message'>
          <p>The PDF preview does not work inside VS Code. Open the preview in a browser:</p>
          <p><a href=${window.location.href} target='_blank' rel='noopener'>${window.location.href}</a></p>
        </div>
      `:this._state==="error"?c`
        <div class='message'>
          <p>The PDF could not be built.</p>
          <pre class='error'>${this._error}</pre>
        </div>
      `:this._state==="building"?c`<div class='message'>Building the PDF… This can take a minute.</div>`:navigator.pdfViewerEnabled===!1?c`<div class='message'>Your browser does not show PDFs inline. Use Download.</div>`:c`<iframe class='viewer' title='PDF preview' src=${this._url}></iframe>`}_onOpen(e){if(!this._open){if(this._reset(),this._name=e.detail.name,this._open=!0,fn()){this._state="embedded";return}this._build(e.detail.kind)}}_build(e){var t=this,n=new AbortController;this._controller=n,this._state="building";var r=T()+"/__preview/pdf?kind="+encodeURIComponent(e)+"&name="+encodeURIComponent(this._name);fetch(r,{method:"POST",signal:n.signal}).then(function(i){return i.json().catch(function(){return{}}).then(function(s){if(n===t._controller){if(t._warnings=s.warnings||[],!s.pdf)throw new Error(s.error||"HTTP "+i.status);t._url=URL.createObjectURL(vn(s.pdf)),t._state="ready"}})}).catch(function(i){n===t._controller&&(t._error=i.message||String(i),t._state="error")})}_reset(){this._controller&&(this._controller.abort(),this._controller=null),this._url&&(URL.revokeObjectURL(this._url),this._url=""),this._error="",this._warnings=[]}_onKeydown(e){this._open&&e.key==="Escape"&&this._close()}_close(){this._reset(),this._open=!1}_stop(e){e.stopPropagation()}};customElements.define("bino-pdf-modal",Ie);var mn=[25,50,100,250],Et=20,St="bino-explorer-layout",he="bino-explorer-sql",He="bino-explorer-history",pe=180,be=560,ne=64,gn=140;function j(o,e,t){return Math.min(Math.max(o,e),t)}function re(o,e){try{var t=window.localStorage.getItem(o);return t!=null?JSON.parse(t):e}catch{return e}}function fe(o,e){try{window.localStorage.setItem(o,JSON.stringify(e))}catch{}}var qe=class extends z{static properties={_open:{state:!0},_metadata:{state:!0},_sql:{state:!0},_result:{state:!0},_summarizeResult:{state:!0},_loading:{state:!0},_error:{state:!0},_page:{state:!0},_pageSize:{state:!0},_activeTab:{state:!0},_expandedSource:{state:!0},_refreshing:{state:!0},_sidebarWidth:{state:!0},_editorHeight:{state:!0},_dragging:{state:!0},_history:{state:!0},_historyOpen:{state:!0},_exporting:{state:!0}};static styles=L`
    :host {
      font-family: var(--bino-font-sans);
    }
    .backdrop {
      position: fixed;
      inset: 0;
      background: var(--bino-scrim);
      z-index: var(--bino-z-modal);
      display: flex;
      flex-direction: column;
    }
    .explorer {
      display: flex;
      flex-direction: column;
      width: 100%;
      height: 100%;
      background: var(--bino-surface);
    }
    .explorer.dragging {
      user-select: none;
      -webkit-user-select: none;
    }
    .explorer-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
      background: var(--bino-surface);
    }
    .explorer-header h2 {
      margin: 0;
      font-size: var(--bino-font-size-md);
      font-weight: 600;
      color: var(--bino-text);
    }
    .header-actions {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
    }
    .refresh-btn {
      display: inline-flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: 4px 10px;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface);
      font-size: var(--bino-font-size-xs);
      font-weight: 600;
      font-family: var(--bino-font-sans);
      cursor: pointer;
      color: var(--bino-text-secondary);
    }
    .refresh-btn:hover {
      background: var(--bino-surface-hover);
      border-color: var(--bino-border-hover);
      color: var(--bino-text);
    }
    .refresh-btn.refreshing {
      opacity: 0.6;
      cursor: not-allowed;
    }
    .refresh-icon {
      display: inline-block;
      font-size: var(--bino-font-size-md);
      line-height: 1;
    }
    .refresh-btn.refreshing .refresh-icon {
      animation: spin 0.8s linear infinite;
    }
    @keyframes spin {
      to { transform: rotate(360deg); }
    }
    @media (prefers-reduced-motion: reduce) {
      .refresh-btn.refreshing .refresh-icon {
        animation: none;
      }
    }
    .close-btn {
      background: none;
      border: none;
      font-size: 20px;
      cursor: pointer;
      color: var(--bino-text-secondary);
      padding: 0 4px;
      line-height: 1;
    }
    .close-btn:hover {
      color: var(--bino-text);
    }
    .explorer-body {
      display: flex;
      flex: 1;
      min-height: 0;
      overflow: hidden;
    }
    .sidebar {
      border-right: none;
      overflow-y: auto;
      background: var(--bino-surface-subtle);
      flex-shrink: 0;
    }
    .splitter-v {
      width: 5px;
      flex-shrink: 0;
      cursor: col-resize;
      background: var(--bino-border);
      transition: background 0.15s;
      touch-action: none;
    }
    .splitter-v:hover,
    .splitter-v:focus-visible,
    .splitter-v.active {
      background: var(--bino-primary);
      outline: none;
    }
    .splitter-h {
      height: 5px;
      flex-shrink: 0;
      cursor: row-resize;
      background: var(--bino-border);
      transition: background 0.15s;
      touch-action: none;
    }
    .splitter-h:hover,
    .splitter-h:focus-visible,
    .splitter-h.active {
      background: var(--bino-primary);
      outline: none;
    }
    .sidebar-section {
      padding: var(--bino-space-sm) 0;
    }
    .sidebar-title {
      padding: var(--bino-space-xs) var(--bino-space-md);
      font-size: var(--bino-font-size-xs);
      font-weight: 700;
      text-transform: uppercase;
      color: var(--bino-primary);
      letter-spacing: 0.05em;
    }
    .sidebar-item {
      display: flex;
      align-items: center;
      gap: var(--bino-space-xs);
      padding: 5px var(--bino-space-md);
      cursor: pointer;
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text);
      border-left: 3px solid transparent;
    }
    .sidebar-item:hover {
      background: var(--bino-surface-hover);
      border-left-color: var(--bino-border-light);
    }
    .sidebar-item-name {
      flex: 1;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      font-weight: 500;
    }
    .sidebar-item-badge {
      flex-shrink: 0;
      padding: 1px 5px;
      border-radius: 3px;
      font-size: 10px;
      font-weight: 600;
      text-transform: uppercase;
    }
    .badge-source {
      background: var(--bino-bad-soft);
      color: var(--bino-bad);
    }
    .badge-dataset {
      background: var(--bino-yellow-200);
      color: var(--bino-gray-800);
    }
    .sidebar-info-btn {
      flex-shrink: 0;
      background: none;
      border: none;
      cursor: pointer;
      color: var(--bino-text-secondary);
      font-size: 14px;
      padding: 0 2px;
      line-height: 1;
    }
    .sidebar-info-btn:hover {
      color: var(--bino-primary);
    }
    .column-list {
      padding: 2px var(--bino-space-md) var(--bino-space-xs) calc(var(--bino-space-md) + 12px);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
    }
    .column-entry {
      display: flex;
      justify-content: space-between;
      padding: 1px 0;
    }
    .column-name {
      font-weight: 500;
      color: var(--bino-text-muted);
    }
    .column-type {
      font-style: italic;
      color: var(--bino-text-secondary);
    }
    .main-panel {
      flex: 1;
      display: flex;
      flex-direction: column;
      min-width: 0;
      overflow: hidden;
    }
    .editor-area {
      display: flex;
      flex-direction: column;
      flex-shrink: 0;
      min-height: 0;
    }
    .sql-editor {
      width: 100%;
      flex: 1;
      min-height: 0;
      padding: var(--bino-space-sm) var(--bino-space-md);
      border: none;
      outline: none;
      resize: none;
      font-family: var(--bino-font-mono);
      font-size: var(--bino-font-size-sm);
      line-height: 1.5;
      background: var(--bino-gray-900);
      color: var(--bino-gray-100);
      box-sizing: border-box;
    }
    .sql-editor::placeholder {
      color: var(--bino-gray-500);
    }
    .editor-toolbar {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-xs) var(--bino-space-md);
      background: var(--bino-surface-inset);
      border-top: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .editor-btn {
      padding: 4px 12px;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface);
      font-size: var(--bino-font-size-xs);
      font-weight: 600;
      font-family: var(--bino-font-sans);
      cursor: pointer;
      color: var(--bino-text-muted);
    }
    .editor-btn:hover {
      background: var(--bino-surface-hover);
      border-color: var(--bino-border-hover);
    }
    .editor-btn.primary {
      background: var(--bino-accent);
      border-color: var(--bino-accent-strong);
      color: var(--bino-on-accent);
    }
    .editor-btn.primary:hover {
      background: var(--bino-accent-strong);
    }
    .editor-btn:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
    .editor-shortcut {
      font-size: 10px;
      color: var(--bino-text-secondary);
      margin-left: auto;
    }
    .history-wrap {
      position: relative;
    }
    .history-menu {
      position: absolute;
      top: calc(100% + 4px);
      left: 0;
      z-index: 10;
      min-width: 320px;
      max-width: 520px;
      max-height: 320px;
      overflow-y: auto;
      background: var(--bino-surface);
      border: 1px solid var(--bino-border);
      border-radius: var(--bino-radius);
      box-shadow: var(--bino-shadow-dropdown);
    }
    .history-item {
      display: flex;
      align-items: baseline;
      gap: var(--bino-space-sm);
      width: 100%;
      padding: 6px var(--bino-space-md);
      border: none;
      border-bottom: 1px solid var(--bino-border);
      background: none;
      cursor: pointer;
      text-align: left;
      font-family: var(--bino-font-sans);
    }
    .history-item:last-child {
      border-bottom: none;
    }
    .history-item:hover {
      background: var(--bino-surface-hover);
    }
    .history-sql {
      flex: 1;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      font-family: var(--bino-font-mono);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text);
    }
    .history-time {
      flex-shrink: 0;
      font-size: 10px;
      color: var(--bino-text-secondary);
    }
    .history-empty {
      padding: var(--bino-space-md);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      text-align: center;
    }
    .results-area {
      flex: 1;
      display: flex;
      flex-direction: column;
      min-height: 0;
      overflow: hidden;
    }
    .tab-bar {
      display: flex;
      gap: 0;
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
      background: var(--bino-surface-subtle);
    }
    .tab-btn {
      padding: var(--bino-space-xs) var(--bino-space-md);
      border: none;
      background: none;
      font-size: var(--bino-font-size-sm);
      font-family: var(--bino-font-sans);
      color: var(--bino-text-secondary);
      cursor: pointer;
      border-bottom: 2px solid transparent;
      font-weight: 500;
    }
    .tab-btn:hover {
      color: var(--bino-text);
      background: var(--bino-surface-hover);
    }
    .tab-btn.active {
      color: var(--bino-primary);
      border-bottom-color: var(--bino-primary);
      font-weight: 600;
    }
    .table-container {
      flex: 1;
      overflow: auto;
      min-height: 0;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      font-size: var(--bino-font-size-sm);
    }
    thead {
      position: sticky;
      top: 0;
      z-index: 1;
    }
    th {
      background: var(--bino-surface-inset);
      padding: 6px 12px;
      text-align: left;
      font-weight: 600;
      color: var(--bino-text-muted);
      border-bottom: 2px solid var(--bino-border);
      white-space: nowrap;
      font-size: var(--bino-font-size-xs);
    }
    .col-type {
      font-weight: 400;
      color: var(--bino-text-secondary);
      font-style: italic;
      margin-left: 4px;
    }
    td {
      padding: 4px 12px;
      border-bottom: 1px solid var(--bino-border);
      max-width: 300px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      color: var(--bino-text);
    }
    .row-num {
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-xs);
      text-align: right;
      width: 1%;
      user-select: none;
    }
    tr:nth-child(even) {
      background: var(--bino-surface-subtle);
    }
    tr:hover {
      background: var(--bino-surface-hover);
    }
    .pagination {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-xs) var(--bino-space-md);
      border-top: 1px solid var(--bino-border);
      flex-shrink: 0;
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      background: var(--bino-surface-subtle);
    }
    .pagination button {
      padding: 2px 8px;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      background: var(--bino-surface);
      cursor: pointer;
      font-size: var(--bino-font-size-xs);
      font-family: var(--bino-font-sans);
      color: var(--bino-text-muted);
    }
    .pagination button:hover:not(:disabled) {
      background: var(--bino-surface-hover);
    }
    .pagination button:disabled {
      opacity: 0.4;
      cursor: not-allowed;
    }
    .pagination select {
      padding: 2px 4px;
      border-radius: var(--bino-radius);
      border: 1px solid var(--bino-border-light);
      font-size: var(--bino-font-size-xs);
      font-family: var(--bino-font-sans);
      color: var(--bino-text-muted);
      background: var(--bino-surface);
    }
    .status-bar {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-xs) var(--bino-space-md);
      border-top: 1px solid var(--bino-border);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      background: var(--bino-surface-subtle);
      flex-shrink: 0;
    }
    .error-msg {
      padding: var(--bino-space-md);
      color: var(--bino-error);
      font-size: var(--bino-font-size-sm);
      white-space: pre-wrap;
      word-break: break-word;
    }
    .loading-msg {
      padding: var(--bino-space-lg);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-sm);
    }
    .empty-state {
      padding: var(--bino-space-xl);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
    }
    .duration {
      color: var(--bino-text-secondary);
    }
  `;constructor(){super(),this._open=!1,this._metadata=null,this._sql=typeof re(he,"")=="string"?re(he,""):"",this._result=null,this._summarizeResult=null,this._loading=!1,this._error="",this._page=0,this._pageSize=50,this._activeTab="results",this._expandedSource=null,this._refreshing=!1;var e=re(St,{});this._sidebarWidth=j(Number(e.sidebarWidth)||280,pe,be),this._editorHeight=Math.max(Number(e.editorHeight)||160,ne),this._dragging=!1,this._history=Array.isArray(re(He,[]))?re(He,[]):[],this._historyOpen=!1,this._exporting=!1,this._boundOnOpen=this._onOpen.bind(this),this._boundOnKeydown=this._onKeydown.bind(this),this._boundOnDocsChanged=this._onDocsChanged.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-open-explorer",this._boundOnOpen),document.addEventListener("keydown",this._boundOnKeydown),document.addEventListener("bino-documents-changed",this._boundOnDocsChanged)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-open-explorer",this._boundOnOpen),document.removeEventListener("keydown",this._boundOnKeydown),document.removeEventListener("bino-documents-changed",this._boundOnDocsChanged)}render(){return this._open?c`
      <div class="backdrop">
        <div class="explorer ${this._dragging?"dragging":""}" @click=${this._onExplorerClick}>
          <div class="explorer-header">
            <h2>Data Explorer</h2>
            <div class="header-actions">
              <button class="refresh-btn ${this._refreshing?"refreshing":""}"
                title="Refresh data sources and datasets"
                ?disabled=${this._refreshing}
                @click=${this._onRefresh}>
                <span class="refresh-icon">↻</span>
                <span>${this._refreshing?"Refreshing...":"Refresh"}</span>
              </button>
              <button class="close-btn" title="Close (Esc)" @click=${this._close}>&times;</button>
            </div>
          </div>
          <div class="explorer-body">
            ${this._renderSidebar()}
            <div class="splitter-v ${this._dragging==="sidebar"?"active":""}"
              role="separator" aria-orientation="vertical" aria-label="Resize sidebar" tabindex="0"
              title="Drag to resize the sidebar (arrow keys when focused)"
              @pointerdown=${this._startSidebarDrag}
              @keydown=${this._onSidebarSplitterKey}></div>
            ${this._renderMainPanel()}
          </div>
        </div>
      </div>
    `:k}_renderSidebar(){var e=this,t=this._metadata,n="width: "+this._sidebarWidth+"px; min-width: "+this._sidebarWidth+"px;";return t?c`
      <div class="sidebar" style=${n}>
        ${t.sources&&t.sources.length>0?c`
          <div class="sidebar-section">
            <div class="sidebar-title">DataSources (${t.sources.length})</div>
            ${t.sources.map(function(r){var i=e._expandedSource===r.name;return c`
                <div>
                  <div class="sidebar-item" @click=${function(){e._selectSource(r.name)}}>
                    <span class="sidebar-item-name">${r.name}</span>
                    ${r.type?c`<span class="sidebar-item-badge badge-source">${r.type}</span>`:k}
                    <button class="sidebar-info-btn" title="Show columns"
                      @click=${function(s){s.stopPropagation(),e._toggleSourceInfo(r.name)}}>
                      ${i?"\u25B4":"\u25BE"}
                    </button>
                  </div>
                  ${i&&r.columns&&r.columns.length>0?c`
                    <div class="column-list">
                      ${r.columns.map(function(s){return c`
                          <div class="column-entry">
                            <span class="column-name">${s.name}</span>
                            <span class="column-type">${s.type}</span>
                          </div>
                        `})}
                    </div>
                  `:k}
                </div>
              `})}
          </div>
        `:k}
        ${t.datasets&&t.datasets.length>0?c`
          <div class="sidebar-section">
            <div class="sidebar-title">DataSets (${t.datasets.length})</div>
            ${t.datasets.map(function(r){return c`
                <div class="sidebar-item" @click=${function(){e._selectDataset(r)}}
                     title=${r.sqlError?"cannot resolve dataset SQL: "+r.sqlError:r.sql||""}>
                  <span class="sidebar-item-name">${r.name}</span>
                  <span class="sidebar-item-badge badge-dataset">set</span>
                </div>
              `})}
          </div>
        `:k}
        ${(!t.sources||t.sources.length===0)&&(!t.datasets||t.datasets.length===0)?c`<div class="empty-state">No data sources found</div>`:k}
      </div>
    `:c`<div class="sidebar" style=${n}><div class="loading-msg">Loading...</div></div>`}_renderMainPanel(){var e=this;return c`
      <div class="main-panel">
        <div class="editor-area" style="height: ${this._editorHeight}px;">
          <textarea class="sql-editor"
            placeholder="Enter SQL query... (Ctrl+Enter to run)"
            .value=${this._sql}
            @input=${this._onSqlInput.bind(this)}
            @keydown=${this._onEditorKeydown.bind(this)}
            spellcheck="false"
          ></textarea>
          <div class="editor-toolbar">
            <button class="editor-btn primary" ?disabled=${this._loading} @click=${this._runQuery.bind(this)}>Run</button>
            <button class="editor-btn" ?disabled=${this._loading||!this._sql.trim()} @click=${this._runSummarize.bind(this)}>Summarize</button>
            <div class="history-wrap">
              <button class="editor-btn" title="Recent queries"
                @click=${function(t){t.stopPropagation(),e._historyOpen=!e._historyOpen}}>History ▾</button>
              ${this._historyOpen?this._renderHistoryMenu():k}
            </div>
            <button class="editor-btn" ?disabled=${this._exporting||!this._sql.trim()}
              title="Download the full result set as CSV"
              @click=${this._exportCSV.bind(this)}>${this._exporting?"Exporting...":"Export CSV"}</button>
            <button class="editor-btn" @click=${this._clearEditor.bind(this)}>Clear</button>
            <span class="editor-shortcut">${navigator.platform.includes("Mac")?"\u2318":"Ctrl"}+Enter to run</span>
          </div>
        </div>
        <div class="splitter-h ${this._dragging==="editor"?"active":""}"
          role="separator" aria-orientation="horizontal" aria-label="Resize SQL editor" tabindex="0"
          title="Drag to resize the SQL editor (arrow keys when focused)"
          @pointerdown=${this._startEditorDrag}
          @keydown=${this._onEditorSplitterKey}></div>
        <div class="results-area">
          <div class="tab-bar">
            <button class="tab-btn ${this._activeTab==="results"?"active":""}"
              @click=${function(){e._activeTab="results"}}>Results</button>
            <button class="tab-btn ${this._activeTab==="summarize"?"active":""}"
              @click=${function(){e._activeTab="summarize"}}>Summarize</button>
          </div>
          ${this._activeTab==="results"?this._renderResults():this._renderSummarizeTab()}
        </div>
      </div>
    `}_renderHistoryMenu(){var e=this;return this._history.length?c`
      <div class="history-menu" @click=${function(t){t.stopPropagation()}}>
        ${this._history.map(function(t){return c`
            <button class="history-item" title=${t.sql}
              @click=${function(){e._applyHistoryEntry(t)}}>
              <span class="history-sql">${t.sql}</span>
              <span class="history-time">${e._formatHistoryTime(t.ts)}</span>
            </button>
          `})}
      </div>
    `:c`<div class="history-menu"><div class="history-empty">No queries yet</div></div>`}_renderResults(){if(this._loading)return c`<div class="loading-msg">Executing query...</div>`;if(this._error)return c`<div class="error-msg">${this._error}</div>`;if(!this._result)return c`<div class="empty-state">Run a query to see results</div>`;if(this._result.error)return c`
        <div class="error-msg">${this._result.error}</div>
        ${this._result.durationMs!=null?c`<div class="status-bar"><span class="duration">${this._result.durationMs}ms</span></div>`:k}
      `;var e=this,t=this._result.columns||[],n=this._result.rows||[],r=this._result.totalRows,i=typeof r=="number"?Math.ceil(r/this._pageSize):null,s=this._page*this._pageSize;return c`
      <div class="table-container">
        ${t.length===0?c`<div class="empty-state">No columns returned</div>`:c`
            <table>
              <thead>
                <tr>
                  <th class="row-num">#</th>
                  ${t.map(function(u){return c`<th>${u.name}<span class="col-type">${u.type}</span></th>`})}
                </tr>
              </thead>
              <tbody>
                ${n.map(function(u,a){return c`<tr>
                    <td class="row-num">${s+a+1}</td>
                    ${u.map(function(p){return c`<td title="${p!=null?String(p):""}">${p!=null?String(p):""}</td>`})}
                  </tr>`})}
              </tbody>
            </table>
          `}
      </div>
      <div class="pagination">
        <button ?disabled=${this._page===0} @click=${function(){e._page--,e._rerunQuery()}}>←</button>
        <span>Page ${this._page+1}${i!=null?" of "+i:""}</span>
        <button ?disabled=${i!=null&&this._page>=i-1} @click=${function(){e._page++,e._rerunQuery()}}>→</button>
        <span>|</span>
        <select .value=${String(this._pageSize)} @change=${function(u){e._pageSize=parseInt(u.target.value,10),e._page=0,e._rerunQuery()}}>
          ${mn.map(function(u){return c`<option value=${u} ?selected=${u===e._pageSize}>${u} rows</option>`})}
        </select>
        <span class="duration">${this._result.durationMs!=null?this._result.durationMs+"ms":""}</span>
        ${typeof r=="number"?c`<span>${r} total rows</span>`:k}
      </div>
    `}_renderSummarizeTab(){if(this._loading)return c`<div class="loading-msg">Running SUMMARIZE...</div>`;if(!this._summarizeResult)return c`<div class="empty-state">Click "Summarize" to see column statistics</div>`;if(this._summarizeResult.error)return c`<div class="error-msg">${this._summarizeResult.error}</div>`;var e=this._summarizeResult.columns||[],t=this._summarizeResult.rows||[];return c`
      <div class="table-container">
        <table>
          <thead>
            <tr>
              ${e.map(function(n){return c`<th>${n.name}<span class="col-type">${n.type}</span></th>`})}
            </tr>
          </thead>
          <tbody>
            ${t.map(function(n){return c`<tr>${n.map(function(r){return c`<td title="${r!=null?String(r):""}">${r!=null?String(r):""}</td>`})}</tr>`})}
          </tbody>
        </table>
      </div>
      <div class="status-bar">
        <span class="duration">${this._summarizeResult.durationMs!=null?this._summarizeResult.durationMs+"ms":""}</span>
      </div>
    `}_onOpen(){this._open=!0,this._fetchMetadata()}_close(){this._open=!1,this._historyOpen=!1}_onKeydown(e){if(this._open&&e.key==="Escape"){if(this._historyOpen){this._historyOpen=!1;return}this._close()}}_onExplorerClick(){this._historyOpen&&(this._historyOpen=!1)}_onDocsChanged(){this._open&&this._fetchMetadata()}_onRefresh(){if(!this._refreshing){this._refreshing=!0;var e=this;this._fetchMetadata(function(){e._refreshing=!1})}}_onSqlInput(e){this._sql=e.target.value,fe(he,this._sql)}_setSql(e){this._sql=e,fe(he,e)}_maxEditorHeight(){var e=this.renderRoot.querySelector(".main-panel");return e?Math.max(e.clientHeight-gn,ne):600}_startEditorDrag(e){e.preventDefault();var t=this,n=e.clientY,r=this._editorHeight,i=this._maxEditorHeight();this._dragging="editor";function s(a){t._editorHeight=j(r+(a.clientY-n),ne,i)}function u(){window.removeEventListener("pointermove",s),window.removeEventListener("pointerup",u),t._dragging=!1,t._saveLayout()}window.addEventListener("pointermove",s),window.addEventListener("pointerup",u)}_startSidebarDrag(e){e.preventDefault();var t=this,n=e.clientX,r=this._sidebarWidth;this._dragging="sidebar";function i(u){t._sidebarWidth=j(r+(u.clientX-n),pe,be)}function s(){window.removeEventListener("pointermove",i),window.removeEventListener("pointerup",s),t._dragging=!1,t._saveLayout()}window.addEventListener("pointermove",i),window.addEventListener("pointerup",s)}_onEditorSplitterKey(e){var t=16;e.key==="ArrowUp"?(e.preventDefault(),this._editorHeight=j(this._editorHeight-t,ne,this._maxEditorHeight()),this._saveLayout()):e.key==="ArrowDown"&&(e.preventDefault(),this._editorHeight=j(this._editorHeight+t,ne,this._maxEditorHeight()),this._saveLayout())}_onSidebarSplitterKey(e){var t=16;e.key==="ArrowLeft"?(e.preventDefault(),this._sidebarWidth=j(this._sidebarWidth-t,pe,be),this._saveLayout()):e.key==="ArrowRight"&&(e.preventDefault(),this._sidebarWidth=j(this._sidebarWidth+t,pe,be),this._saveLayout())}_saveLayout(){fe(St,{sidebarWidth:this._sidebarWidth,editorHeight:this._editorHeight})}_recordHistory(e){var t={sql:e,ts:Date.now()},n=this._history.filter(function(r){return r.sql!==e});n.unshift(t),n.length>Et&&(n.length=Et),this._history=n,fe(He,n)}_applyHistoryEntry(e){this._setSql(e.sql),this._historyOpen=!1,this._page=0,this._runQuery()}_formatHistoryTime(e){if(!e)return"";var t=new Date(e),n=new Date,r=t.toDateString()===n.toDateString();return r?t.toLocaleTimeString([],{hour:"2-digit",minute:"2-digit"}):t.toLocaleDateString([],{month:"short",day:"numeric"})}_onEditorKeydown(e){if(e.key==="Tab"){e.preventDefault();var t=e.target,n=t.selectionStart,r=t.selectionEnd;this._setSql(this._sql.substring(0,n)+"  "+this._sql.substring(r)),this.updateComplete.then(function(){t.selectionStart=t.selectionEnd=n+2});return}e.key==="Enter"&&(e.metaKey||e.ctrlKey)&&(e.preventDefault(),this._runQuery())}_selectSource(e){this._setSql('SELECT * FROM "'+e+'"'),this._page=0,this._activeTab="results",this._runQuery()}_selectDataset(e){e&&e.sql?this._setSql(e.sql):e&&e.sqlError?this._setSql('-- Cannot resolve SQL for dataset "'+e.name+`":
-- `+e.sqlError):this._setSql('-- Dataset "'+(e&&e.name)+'" has no resolvable SQL.'),this._page=0,this._activeTab="results",e&&e.sql&&this._runQuery()}_toggleSourceInfo(e){this._expandedSource=this._expandedSource===e?null:e}_clearEditor(){this._setSql(""),this._result=null,this._summarizeResult=null,this._error="",this._page=0}_rerunQuery(){this._sql.trim()&&this._runQuery()}_runQuery(){var e=this._sql.trim();if(e){var t=this;this._loading=!0,this._error="",this._activeTab="results",this._recordHistory(e),fetch(T()+"/__explorer/query",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({sql:e,limit:this._pageSize,offset:this._page*this._pageSize})}).then(function(n){return n.json()}).then(function(n){t._result=n,t._loading=!1}).catch(function(n){t._error=n.message||"Query failed",t._loading=!1})}}_runSummarize(){var e=this._sql.trim();if(e){var t=this;this._loading=!0,this._error="",this._activeTab="summarize",fetch(T()+"/__explorer/summarize",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({sql:e})}).then(function(n){return n.json()}).then(function(n){t._summarizeResult=n,t._loading=!1}).catch(function(n){t._error=n.message||"Summarize failed",t._loading=!1})}}_exportCSV(){var e=this._sql.trim();if(!(!e||this._exporting)){var t=this;this._exporting=!0,fetch(T()+"/__explorer/export",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({sql:e})}).then(function(n){var r=n.headers.get("Content-Type")||"";return r.indexOf("text/csv")===-1?n.json().then(function(i){throw new Error(i.error||"Export failed")}):n.blob()}).then(function(n){var r=URL.createObjectURL(n),i=document.createElement("a");i.href=r,i.download="bino-explorer-export.csv",i.click(),URL.revokeObjectURL(r),t._exporting=!1}).catch(function(n){t._error=n.message||"Export failed",t._activeTab="results",t._exporting=!1})}}_fetchMetadata(e){var t=this;fetch(T()+"/__explorer/metadata").then(function(n){return n.json()}).then(function(n){t._metadata=n,e&&e()}).catch(function(n){console.error("explorer: fetch metadata failed",n),e&&e()})}};customElements.define("bino-data-explorer",qe);var _n=3,Ue=class extends z{static properties={_open:{state:!0},_supported:{state:!0},_state:{state:!0},_findings:{state:!0},_elements:{state:!0},_selectedId:{state:!0},_detail:{state:!0},_showBoxes:{state:!0},_provisional:{state:!0},_busy:{state:!0},_error:{state:!0}};static styles=L`
    :host {
      position: fixed;
      top: var(--bino-toolbar-height);
      right: 0;
      bottom: 0;
      width: var(--bino-sidebar-width);
      max-width: 100vw;
      z-index: var(--bino-z-panel);
      background: var(--bino-surface);
      border-left: 1px solid var(--bino-border);
      box-shadow: var(--bino-shadow-dropdown);
      font-family: var(--bino-font-sans);
      font-size: var(--bino-font-size-sm);
      color: var(--bino-text);
      display: none;
      flex-direction: column;
    }
    :host([open]) {
      display: flex;
    }
    .header {
      display: flex;
      align-items: center;
      gap: var(--bino-space-sm);
      padding: var(--bino-space-sm) var(--bino-space-md);
      border-bottom: 1px solid var(--bino-border);
      font-weight: 600;
      color: var(--bino-text-muted);
      flex-shrink: 0;
    }
    .header .spacer {
      flex: 1;
    }
    .icon-btn {
      background: none;
      border: none;
      cursor: pointer;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-md);
      padding: 2px 4px;
      border-radius: var(--bino-radius);
      font-family: inherit;
    }
    .icon-btn:hover {
      background: var(--bino-surface-hover);
      color: var(--bino-text);
    }
    .icon-btn[aria-pressed='true'] {
      background: var(--bino-surface-active);
      color: var(--bino-active-text);
    }
    .body {
      overflow-y: auto;
      flex: 1;
    }
    .notice {
      padding: var(--bino-space-md);
      color: var(--bino-text-secondary);
      line-height: 1.5;
    }
    .notice.provisional {
      background: var(--bino-warning-bg);
      border-bottom: 1px solid var(--bino-warning-border);
      color: var(--bino-warning-text);
      margin: 0;
      padding: var(--bino-space-sm) var(--bino-space-md);
    }
    .notice code {
      font-family: var(--bino-font-mono);
      font-size: var(--bino-font-size-xs);
      background: var(--bino-surface-inset);
      padding: 1px 4px;
      border-radius: 4px;
    }
    .section-title {
      padding: var(--bino-space-sm) var(--bino-space-md) var(--bino-space-xs);
      font-size: var(--bino-font-size-xs);
      text-transform: uppercase;
      letter-spacing: 0.04em;
      color: var(--bino-text-secondary);
      font-weight: 600;
    }
    ul {
      list-style: none;
      margin: 0;
      padding: 0;
    }
    .finding {
      padding: var(--bino-space-sm) var(--bino-space-md);
      border-left: 3px solid var(--bino-warning);
      background: var(--bino-warning-bg);
      border-bottom: 1px solid var(--bino-warning-border);
      cursor: pointer;
      line-height: 1.4;
    }
    .finding.error {
      border-left-color: var(--bino-error);
      background: var(--bino-error-bg);
      border-bottom-color: var(--bino-error-border);
    }
    .finding-label {
      font-weight: 600;
      color: var(--bino-text-muted);
    }
    .finding-hint {
      display: block;
      margin-top: 2px;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-xs);
    }
    .row {
      padding: var(--bino-space-sm) var(--bino-space-md);
      border-bottom: 1px solid var(--bino-border);
      cursor: pointer;
    }
    .row:hover {
      background: var(--bino-surface-hover);
    }
    .row.selected {
      background: var(--bino-surface-active);
    }
    .row-label {
      font-weight: 600;
      display: flex;
      align-items: baseline;
      gap: var(--bino-space-xs);
    }
    .row-tag {
      font-family: var(--bino-font-mono);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
      font-weight: 400;
    }
    .chips {
      display: flex;
      flex-wrap: wrap;
      gap: var(--bino-space-xs);
      margin-top: var(--bino-space-xs);
    }
    .chip {
      padding: 1px 6px;
      border-radius: var(--bino-radius-pill);
      font-size: var(--bino-font-size-xs);
      background: var(--bino-surface-inset);
      color: var(--bino-text-secondary);
      white-space: nowrap;
    }
    .chip.scale {
      background: var(--bino-celeste-100);
      color: var(--bino-celeste-800);
    }
    .chip.warn {
      background: var(--bino-warning-bg);
      color: var(--bino-warning-text);
    }
    .chip.bad {
      background: var(--bino-error-bg);
      color: var(--bino-bad);
    }
    .detail {
      padding: var(--bino-space-sm) var(--bino-space-md);
      background: var(--bino-surface-subtle);
      border-bottom: 1px solid var(--bino-border);
      line-height: 1.5;
    }
    .detail dl {
      margin: 0;
      display: grid;
      grid-template-columns: auto 1fr;
      gap: 2px var(--bino-space-sm);
    }
    .detail dt {
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-xs);
    }
    .detail dd {
      margin: 0;
      font-family: var(--bino-font-mono);
      font-size: var(--bino-font-size-xs);
      overflow-wrap: anywhere;
    }
    .detail-actions {
      margin-top: var(--bino-space-sm);
      display: flex;
      gap: var(--bino-space-xs);
    }
    .text-btn {
      background: var(--bino-surface);
      border: 1px solid var(--bino-border-light);
      border-radius: var(--bino-radius-pill);
      padding: 2px 10px;
      font-size: var(--bino-font-size-xs);
      font-family: inherit;
      color: var(--bino-text-secondary);
      cursor: pointer;
    }
    .text-btn:hover {
      background: var(--bino-surface-hover);
      border-color: var(--bino-border-hover);
    }
  `;constructor(){super(),this._open=!1,this._supported=!0,this._state=null,this._findings=[],this._elements=new Map,this._selectedId="",this._detail=null,this._showBoxes=!1,this._provisional=!1,this._retries=0,this._busy=!1,this._error="",this._captureTimer=null,this._overlays=[],this._boundOnOpen=this._onOpen.bind(this),this._boundOnContentUpdated=this._onContentUpdated.bind(this),this._boundOnKeydown=this._onKeydown.bind(this)}connectedCallback(){super.connectedCallback(),document.addEventListener("bino-open-inspector",this._boundOnOpen),document.addEventListener("bn-preview:content-updated",this._boundOnContentUpdated),document.addEventListener("keydown",this._boundOnKeydown)}disconnectedCallback(){super.disconnectedCallback(),document.removeEventListener("bino-open-inspector",this._boundOnOpen),document.removeEventListener("bn-preview:content-updated",this._boundOnContentUpdated),document.removeEventListener("keydown",this._boundOnKeydown),this._captureTimer&&clearTimeout(this._captureTimer),this._clearOverlays()}updated(e){e.has("_open")&&(this._open?this.setAttribute("open",""):this.removeAttribute("open"))}render(){return c`
      <div class="header">
        <span>Inspector</span>
        <span class="spacer"></span>
        ${this._supported?c`
              <button class="icon-btn" title="Outline every component"
                aria-pressed=${this._showBoxes?"true":"false"} @click=${this._onToggleBoxes}>□</button>
              <button class="icon-btn" title="Copy the snapshot as JSON" @click=${this._onCopy}>⎘</button>
              <button class="icon-btn" title="Re-capture" @click=${this._onRefresh}>↻</button>
            `:k}
        <button class="icon-btn" title="Close" @click=${this._onClose}>&times;</button>
      </div>
      <div class="body">${this._renderBody()}</div>
    `}_renderBody(){if(!this._supported)return c`
        <p class="notice">
          This template engine does not expose layout state. The inspector needs
          <code>bn-template-engine v1.0.0-next.24</code> or newer - pin it with
          <code>engine-version</code> in <code>bino.toml</code>.
        </p>
      `;if(this._error)return c`<p class="notice">${this._error}</p>`;if(this._busy&&!this._state)return c`<p class="notice">Capturing…</p>`;if(!this._state)return c`<p class="notice">No snapshot yet.</p>`;var e=this._state.components||[];return c`
      ${this._provisional?c`
            <p class="notice provisional">
              The report is still rendering, so these boxes are provisional and
              no checks were run. Re-capturing…
            </p>
          `:k}
      ${this._findings.length>0?c`
            <div class="section-title">${this._findings.length} finding${this._findings.length===1?"":"s"}</div>
            <ul>${this._findings.map(t=>this._renderFinding(t))}</ul>
          `:k}
      <div class="section-title">${e.length} component${e.length===1?"":"s"}</div>
      <ul>${e.map(t=>this._renderComponent(t))}</ul>
    `}_renderFinding(e){var t=e.name?e.kind?e.kind+" "+e.name:e.name:e.componentId;return c`
      <li class="finding ${e.severity==="error"?"error":""}"
        @click=${()=>this._select(e.componentId)}>
        <span class="finding-label">${t}</span> - ${e.message}
        ${e.hint?c`<span class="finding-hint">${e.hint}</span>`:k}
      </li>
    `}_renderComponent(e){var t=this._state.sources&&this._state.sources[e.id]||{},n=t.name||t.ref||e.id,r=e.id===this._selectedId;return c`
      <li>
        <div class="row ${r?"selected":""}"
          @click=${i=>this._onRowClick(i,e.id)}
          @mouseenter=${()=>this._outline(e.id)}
          @mouseleave=${()=>this._clearHoverOutline()}>
          <div class="row-label">
            <span>${t.kind?t.kind+" "+n:n}</span>
            <span class="row-tag">${e.tag}</span>
          </div>
          <div class="chips">
            <span class="chip">${this._formatBox(e)}</span>
            ${this._countChips(e)}
            ${this._scaleChip(e)}
            ${this._diagChips(e)}
          </div>
        </div>
        ${r?this._renderDetail(e):k}
      </li>
    `}_renderDetail(e){var t=[],n=e.em||{};t.push(["font",this._round(n.fontSizePx)+"px"]),n.appliedScaleFactor!=null&&n.appliedScaleFactor!==1&&t.push(["auto-fit",Math.round(n.appliedScaleFactor*100)+"%"]);var r=e.scaling||{};r.unitsPerEm!=null&&t.push(["units/em",this._round(r.unitsPerEm)+" ("+(r.unitMode||"auto")+")"]),r.percentagePointsPerEm!=null&&t.push(["pp/em",this._round(r.percentagePointsPerEm)+" ("+(r.percentageMode||"auto")+")"]),(e.regions||[]).forEach(function(s){t.push([s.id,Math.round(s.rect.component.width)+"\xD7"+Math.round(s.rect.component.height)])});var i=e.metadata||{};return i.scenarios&&i.scenarios.length>0&&t.push(["scenarios",i.scenarios.join(", ")]),i.variances&&i.variances.length>0&&t.push(["variances",i.variances.join(", ")]),(e.diagnostics||[]).forEach(function(s){t.push([s.id||s.type||"diagnostic",s.message||""])}),c`
      <div class="detail">
        <dl>${t.map(s=>c`<dt>${s[0]}</dt><dd>${s[1]}</dd>`)}</dl>
        ${this._detail?this._renderElementDetail():k}
        <div class="detail-actions">
          ${this._detail?k:c`<button class="text-btn" @click=${()=>this._loadDetail(e.id)}>Element detail</button>`}
          <button class="text-btn" @click=${()=>this._revealSource(e.id)}>Reveal source</button>
        </div>
      </div>
    `}_renderElementDetail(){var e={};(this._detail.elements||[]).forEach(function(r){e[r.kind]=(e[r.kind]||0)+1});var t=Object.keys(e).sort(),n=this._detail.table&&this._detail.table.columns||[];return c`
      <dl>
        ${t.map(r=>c`<dt>${r}</dt><dd>${e[r]}</dd>`)}
        ${n.map(r=>c`<dt>col ${r.index}</dt><dd>${r.key}${r.bucket?" \xB7 "+r.bucket:""}</dd>`)}
      </dl>
    `}_formatBox(e){var t=e.rect&&e.rect.component||{width:0,height:0};return Math.round(t.width)+"\xD7"+Math.round(t.height)}_countChips(e){var t=e.metadata||{},n=[["bars",t.barCount],["points",t.pointCount],["rows",t.rowCount],["nodes",t.nodeCount]];return n.filter(function(r){return r[1]!=null}).map(function(r){return c`<span class="chip ${r[1]===0?"warn":""}">${r[1]} ${r[0]}</span>`})}_scaleChip(e){var t=e.scaling||{};return t.unitsPerEm==null?k:c`<span class="chip scale">${t.unitMode||"auto"} ${this._round(t.unitsPerEm)}/em</span>`}_diagChips(e){return(e.diagnostics||[]).map(function(t){return c`<span class="chip ${t.type==="error"?"bad":"warn"}">${t.id||t.type}</span>`})}_round(e){return e==null?"":Math.round(e*100)/100}_onOpen(){this._open=!0,this._retries=0,this._capture()}_onClose(){this._open=!1,this._showBoxes=!1,this._clearOverlays()}_onKeydown(e){e.key==="Escape"&&this._open&&this._onClose()}_onContentUpdated(){if(this._open){this._captureTimer&&clearTimeout(this._captureTimer);var e=this;this._captureTimer=setTimeout(function(){e._retries=0,e._capture()},250)}}_onRefresh(){this._retries=0,this._capture()}_scheduleRetry(){if(!(this._retries>=_n)){this._retries++;var e=this;this._captureTimer&&clearTimeout(this._captureTimer),this._captureTimer=setTimeout(function(){e._capture()},1e3)}}async _capture(){if(this._supported=ce(),!this._supported){this._state=null;return}this._busy=!0,this._error="";try{var e=await xt({settleTimeoutMs:5e3});if(!e){this._supported=!1;return}var t=this._selectedId;this._elements=e.elements,this._detail=null,this._state=Object.assign({},e.state,{sources:e.sources}),this._selectedId=e.elements.has(t)?t:"",this._provisional=!e.settled,this._provisional?(this._findings=[],this._scheduleRetry()):this._findings=await this._analyze(e),this._showBoxes&&this._drawAllOutlines()}catch(n){console.error("bino inspector: capture failed",n),this._error="Capture failed: "+(n&&n.message?n.message:n)}finally{this._busy=!1}}async _analyze(e){try{var t=await fetch(T()+"/__bino/layout-state",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({state:e.state,sources:e.sources})});if(!t.ok)return console.warn("bino inspector: analysis failed",t.status),[];var n=await t.json();return n.findings||[]}catch(r){return console.warn("bino inspector: analysis request failed",r),[]}}async _loadDetail(e){var t=this._elements.get(e);try{this._detail=await wt(t)||{elements:[]}}catch(n){console.warn("bino inspector: element detail failed",n),this._detail={elements:[]}}}_onRowClick(e,t){if(e.metaKey||e.ctrlKey){this._revealSource(t);return}this._select(t)}_select(e){if(this._selectedId===e){this._selectedId="",this._detail=null;return}this._selectedId=e,this._detail=null;var t=this._elements.get(e);t&&(t.scrollIntoView({behavior:"smooth",block:"center"}),this._outline(e,!0))}_revealSource(e){var t=this._elements.get(e),n=t&&t.closest?t.closest("[data-bino-kind]"):null;if(n){var r={type:"bino:revealSource",kind:n.getAttribute("data-bino-kind"),name:n.getAttribute("data-bino-name")||"",ref:n.getAttribute("data-bino-ref")||""},i=parseInt(n.getAttribute("data-bino-source-line")||"",10);i>0&&(r.sourceFile=n.getAttribute("data-bino-source-file")||"",r.line=i),window.parent&&window.parent!==window&&window.parent.postMessage(r,"*")}}_outline(e,t){var n=this._elements.get(e);n&&(this._showBoxes||this._clearOverlays(),this._overlays.push(this._makeOutline(n,t?"selected":"hover")))}_clearHoverOutline(){this._showBoxes||this._clearOverlays()}_onToggleBoxes(){this._showBoxes=!this._showBoxes,this._clearOverlays(),this._showBoxes&&this._drawAllOutlines()}_drawAllOutlines(){var e=this;this._elements.forEach(function(t){e._overlays.push(e._makeOutline(t,"all"))})}_makeOutline(e,t){var n=e.getBoundingClientRect(),r=document.createElement("div");return r.className="bn-inspect-outline",r.style.cssText="position:absolute;pointer-events:none;left:"+(n.left+window.scrollX)+"px;top:"+(n.top+window.scrollY)+"px;width:"+n.width+"px;height:"+n.height+"px;",t==="selected"&&r.classList.add("selected"),document.body.appendChild(r),r}_clearOverlays(){this._overlays.forEach(function(e){e.parentNode&&e.parentNode.removeChild(e)}),this._overlays=[]}async _onCopy(){if(this._state){var e=JSON.stringify({state:this._state,findings:this._findings},null,2);try{await navigator.clipboard.writeText(e)}catch(t){console.warn("bino inspector: clipboard write failed",t)}}}};customElements.define("bino-inspector",Ue);if(!(!window.EventSource||window.__bnPreviewRuntime)){let o=function(){Ke&&je&&e("initial")},e=function(n){var r=++Fe;fetch(Be+"/__preview/context?path="+encodeURIComponent(N)).then(function(i){return i.ok?i.text():(console.warn("bn preview: context fetch failed",i.status,n),null)}).then(function(i){if(!(!i||r!==Fe)){var s=Ye(i,At);if(s){Ve=!0;try{document.dispatchEvent(new CustomEvent("bn-preview:content-updated",{detail:{path:N}}))}catch(u){console.debug("bn preview: custom event skipped",u)}}else console.warn("bn preview: swapContext returned false; DOM not updated")}}).catch(function(i){console.error("bn preview: context fetch errored",i)})},t=function(){if(N==="/"){document.querySelectorAll(".bn-page-info").forEach(function(p){p.remove()});var n=document.querySelector("bn-context");if(n){var r=n.getAttribute("data-page-meta");if(r){var i;try{i=JSON.parse(r)}catch{return}if(Array.isArray(i)){var s={};i.forEach(function(p){s[p.name]=p});var u=n.shadowRoot||n,a=u.querySelectorAll("bn-layout-page[data-bino-page]");a.length===0&&(a=document.querySelectorAll("bn-layout-page[data-bino-page]")),a.forEach(function(p){var f=p.getAttribute("data-bino-page");if(f){var v=f.split("#")[0],b=s[v]||s[f];if(b){var w=document.createElement("div");w.className="bn-page-info";var $=document.createElement("span");if($.className="bn-page-info-name",$.textContent=f,w.appendChild($),b.constraints&&b.constraints.length>0){var m=document.createElement("span");m.className="bn-page-info-label",m.textContent="constraints:",w.appendChild(m),b.constraints.forEach(function(x){var l=document.createElement("span");l.className="bn-page-info-pill constraint",l.textContent=x,w.appendChild(l)})}if(b.artifacts&&b.artifacts.length>0){var S=document.createElement("span");S.className="bn-page-info-label",S.textContent="used in:",w.appendChild(S),b.artifacts.forEach(function(x){var l=document.createElement("span");l.className="bn-page-info-pill artefact",l.textContent=x,w.appendChild(l)})}p.parentNode.insertBefore(w,p)}}})}}}}};window.__bnPreviewRuntime=!0,console.info("bn preview runtime v14 (plugin reload)"),At=new DOMParser,Be=T(),N=ie(),I=new EventSource(Be+"/__preview/events"),Ke=!1,je=!1,Je().then(function(){je=!0,o()}),I.addEventListener("ready",function(){Ke=!0,document.dispatchEvent(new CustomEvent("bn-preview:refresh-done")),o()}),I.addEventListener("refreshing",function(n){try{var r=JSON.parse(n.data||"{}");document.dispatchEvent(new CustomEvent("bn-preview:refreshing",{detail:r}))}catch{document.dispatchEvent(new CustomEvent("bn-preview:refreshing",{detail:{}}))}}),I.addEventListener("refresh-done",function(n){var r={};try{r=JSON.parse(n.data||"{}")||{}}catch{r={}}var i=Array.isArray(r.paths)?r.paths:[],s=!1;if(i.length>0){for(var u=0;u<i.length;u++)if(W(i[u])===N){s=!0;break}s||(console.warn("bn preview: refresh did not include this view",N,"broadcast paths:",i),document.dispatchEvent(new CustomEvent("bn-preview:no-payload",{detail:{path:N,broadcastPaths:i}})))}s&&!Ve&&e("initial-retry"),document.dispatchEvent(new CustomEvent("bn-preview:refresh-done",{detail:r}))}),Fe=0,Ve=!1,I.addEventListener("path-changed",function(n){var r={};try{r=JSON.parse(n.data||"{}")||{}}catch{return}!r.path||W(r.path)!==N||e("path-changed")}),I.addEventListener("refresh-error",function(n){var r={};try{r=JSON.parse(n.data||"{}")}catch{}r&&r.path&&W(r.path)!==N||(console.error("bn preview: refresh failed",r&&r.message),document.dispatchEvent(new CustomEvent("bn-preview:refresh-error",{detail:r})))}),I.addEventListener("plugins-reloaded",function(){location.reload()}),window.addEventListener("beforeunload",function(){I.close()}),document.addEventListener("click",function(n){if(!(!n.metaKey&&!n.ctrlKey)){var r=n.target.closest("[data-bino-kind], [data-bino-file]");if(r){var i;if(r.hasAttribute("data-bino-kind")){i={type:"bino:revealSource",kind:r.getAttribute("data-bino-kind"),name:r.getAttribute("data-bino-name")||"",ref:r.getAttribute("data-bino-ref")||""};var s=parseInt(r.getAttribute("data-bino-source-line")||"",10);s>0&&(i.sourceFile=r.getAttribute("data-bino-source-file")||"",i.line=s)}else i={type:"bino:revealSource",file:r.getAttribute("data-bino-file")};window.parent&&window.parent!==window&&window.parent.postMessage(i,"*"),n.preventDefault(),n.stopPropagation()}}}),et(window),document.addEventListener("bn-preview:content-updated",function(){t()}),document.readyState==="loading"?document.addEventListener("DOMContentLoaded",t):t()}var At,Be,N,I,Ke,je,Fe,Ve;
/*! Bundled license information:

@lit/reactive-element/css-tag.js:
  (**
   * @license
   * Copyright 2019 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/reactive-element.js:
lit-html/lit-html.js:
lit-element/lit-element.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

lit-html/is-server.js:
  (**
   * @license
   * Copyright 2022 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)
*/
